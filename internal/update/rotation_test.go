package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func TestRotatingVerifierAcceptsPreviousKey(t *testing.T) {
	activePub, _, _ := ed25519.GenerateKey(nil)
	previousPub, previousPriv, _ := ed25519.GenerateKey(nil)

	verifier, err := NewRotatingVerifier(
		"key-active",
		map[string]string{
			"key-active":   base64.StdEncoding.EncodeToString(activePub),
			"key-previous": base64.StdEncoding.EncodeToString(previousPub),
		},
		[]string{"key-previous"},
	)
	if err != nil {
		t.Fatalf("new rotating verifier: %v", err)
	}

	manifest := Manifest{
		KeyID:       "key-previous",
		Version:     "0.2.0",
		ArtifactURL: "http://updates.local/koala.bundle.json",
		SHA256:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CreatedAt:   "2026-03-06T00:00:00Z",
	}
	manifest.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(previousPriv, []byte(ManifestSigningPayload(manifest))))
	if err := verifier.VerifyManifest(manifest); err != nil {
		t.Fatalf("verify manifest with previous key: %v", err)
	}
}

func TestRotatingVerifierRejectsUnknownKeyID(t *testing.T) {
	activePub, _, _ := ed25519.GenerateKey(nil)
	verifier, err := NewRotatingVerifier(
		"key-active",
		map[string]string{"key-active": base64.StdEncoding.EncodeToString(activePub)},
		nil,
	)
	if err != nil {
		t.Fatalf("new rotating verifier: %v", err)
	}

	manifest := Manifest{KeyID: "key-other", Signature: base64.StdEncoding.EncodeToString([]byte("x"))}
	if err := verifier.VerifyManifest(manifest); err == nil {
		t.Fatalf("expected rejection for unknown key_id")
	}
	stats := verifier.UnknownKeyStats()
	if stats.ManifestUnknown["key-other"] != 1 {
		t.Fatalf("expected manifest unknown count for key-other to be 1, got %d", stats.ManifestUnknown["key-other"])
	}
}

func TestRotatingVerifierUnknownKeyAlertHook(t *testing.T) {
	activePub, _, _ := ed25519.GenerateKey(nil)
	verifier, err := NewRotatingVerifier(
		"key-active",
		map[string]string{"key-active": base64.StdEncoding.EncodeToString(activePub)},
		nil,
	)
	if err != nil {
		t.Fatalf("new rotating verifier: %v", err)
	}
	calls := 0
	verifier.SetUnknownKeyAlertHook(func(kind string, keyID string) {
		if kind != "bundle" || keyID != "key-unknown" {
			t.Fatalf("unexpected alert payload kind=%s key_id=%s", kind, keyID)
		}
		calls++
	})
	bundle := Bundle{KeyID: "key-unknown", Signature: base64.StdEncoding.EncodeToString([]byte("x"))}
	if err := verifier.VerifyBundle(bundle); err == nil {
		t.Fatalf("expected bundle unknown key rejection")
	}
	if calls != 1 {
		t.Fatalf("expected alert hook to be called once, got %d", calls)
	}
	stats := verifier.UnknownKeyStats()
	if stats.BundleUnknown["key-unknown"] != 1 {
		t.Fatalf("expected bundle unknown count for key-unknown to be 1, got %d", stats.BundleUnknown["key-unknown"])
	}
	alerts := verifier.RecentUnknownKeyAlerts(10)
	if len(alerts) != 1 {
		t.Fatalf("expected one recent alert, got %d", len(alerts))
	}
	if alerts[0].KeyID != "key-unknown" || alerts[0].Kind != "bundle" {
		t.Fatalf("unexpected alert payload: %#v", alerts[0])
	}
}
