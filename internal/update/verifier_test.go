package update

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestNewEd25519VerifierFromBase64(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString(pub)
	verifier, err := NewEd25519VerifierFromBase64(b64)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	sum := sha256.Sum256([]byte("artifact"))
	manifest := Manifest{
		KeyID:       "key-2026-03",
		Version:     "0.2.0",
		ArtifactURL: "http://updates.local/koala.bundle.json",
		SHA256:      hex.EncodeToString(sum[:]),
		CreatedAt:   freshCreatedAt(),
	}
	_, priv, _ := ed25519.GenerateKey(nil)
	manifest.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(ManifestSigningPayload(manifest))))
	if err := verifier.VerifyManifest(manifest); err == nil {
		t.Fatalf("expected verify error with mismatched key")
	}
}

func TestEd25519VerifierVerifyBundle(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	verifier := &Ed25519Verifier{publicKey: pub}
	encKey := []byte("0123456789abcdef0123456789abcdef")
	bundle, err := EncryptAndSignBundle([]byte("artifact"), "key-2026-03", "0.2.0", "2026-03-06T00:00:00Z", encKey, priv)
	if err != nil {
		t.Fatalf("encrypt bundle: %v", err)
	}
	if err := verifier.VerifyBundle(bundle); err != nil {
		t.Fatalf("verify bundle: %v", err)
	}
}
