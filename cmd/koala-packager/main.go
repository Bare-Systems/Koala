package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Bare-Systems/Koala/internal/update"
)

func main() {
	artifactPath := flag.String("artifact", "", "path to raw artifact file")
	bundleURL := flag.String("bundle-url", "", "download URL where encrypted bundle will be served")
	keyID := flag.String("key-id", "", "signing key identifier")
	version := flag.String("version", "", "package version")
	privateKeyB64 := flag.String("private-key-base64", "", "ed25519 private key in base64 (64-byte key or 32-byte seed)")
	encryptionKeyB64 := flag.String("encryption-key-base64", "", "AES-256-GCM key in base64")
	minOrchestratorVersion := flag.String("min-orchestrator-version", "", "minimum orchestrator version")
	minWorkerVersion := flag.String("min-worker-version", "", "minimum worker version")
	createdAt := flag.String("created-at", "", "manifest timestamp (RFC3339). Defaults to now")
	manifestOutPath := flag.String("manifest-out", "", "manifest output file path")
	bundleOutPath := flag.String("bundle-out", "", "bundle output file path")
	flag.Parse()

	if strings.TrimSpace(*artifactPath) == "" || strings.TrimSpace(*bundleURL) == "" || strings.TrimSpace(*keyID) == "" || strings.TrimSpace(*version) == "" ||
		strings.TrimSpace(*privateKeyB64) == "" || strings.TrimSpace(*encryptionKeyB64) == "" ||
		strings.TrimSpace(*manifestOutPath) == "" || strings.TrimSpace(*bundleOutPath) == "" {
		fmt.Fprintln(os.Stderr, "artifact, bundle-url, key-id, version, private-key-base64, encryption-key-base64, manifest-out, and bundle-out are required")
		os.Exit(2)
	}

	artifact, err := os.ReadFile(*artifactPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read artifact: %v\n", err)
		os.Exit(1)
	}
	ts := strings.TrimSpace(*createdAt)
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}

	privateKey, err := parsePrivateKey(*privateKeyB64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid private key: %v\n", err)
		os.Exit(1)
	}
	encryptionKey, err := update.ParseAES256KeyBase64(*encryptionKeyB64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid encryption key: %v\n", err)
		os.Exit(1)
	}

	bundle, err := update.EncryptAndSignBundle(artifact, *keyID, *version, ts, encryptionKey, privateKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build bundle: %v\n", err)
		os.Exit(1)
	}
	bundleOut, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode bundle: %v\n", err)
		os.Exit(1)
	}
	bundleOut = append(bundleOut, '\n')
	if err := os.WriteFile(*bundleOutPath, bundleOut, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write bundle: %v\n", err)
		os.Exit(1)
	}

	sum := sha256.Sum256(artifact)
	manifest := update.Manifest{
		KeyID:                  *keyID,
		Version:                *version,
		ArtifactURL:            *bundleURL,
		SHA256:                 hex.EncodeToString(sum[:]),
		MinOrchestratorVersion: *minOrchestratorVersion,
		MinWorkerVersion:       *minWorkerVersion,
		CreatedAt:              ts,
	}
	signature := ed25519.Sign(privateKey, []byte(update.ManifestSigningPayload(manifest)))
	manifest.Signature = base64.StdEncoding.EncodeToString(signature)

	manifestOut, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode manifest: %v\n", err)
		os.Exit(1)
	}
	manifestOut = append(manifestOut, '\n')
	if err := os.WriteFile(*manifestOutPath, manifestOut, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write manifest: %v\n", err)
		os.Exit(1)
	}
}

func parsePrivateKey(b64 string) (ed25519.PrivateKey, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}
	switch len(decoded) {
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(decoded), nil
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(decoded), nil
	default:
		return nil, fmt.Errorf("private key must be %d-byte key or %d-byte seed", ed25519.PrivateKeySize, ed25519.SeedSize)
	}
}
