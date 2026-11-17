package manifest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// Manifest represents the cryptographically signed release metadata for an OS image.
type Manifest struct {
	Version        string            `json:"version"`
	ImageName      string            `json:"image_name"`
	TargetArch     string            `json:"target_arch"`
	SHA256         string            `json:"sha256"`
	SizeBytes      int64             `json:"size_bytes"`
	BuildTimestamp time.Time         `json:"build_timestamp"`
	MinHWRevision  string            `json:"min_hw_revision,omitempty"`
	Signature      string            `json:"signature"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// CanonicalPayload returns deterministic bytes over which the Ed25519 signature is evaluated.
func (m *Manifest) CanonicalPayload() []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%s|%d|%d",
		m.Version,
		m.ImageName,
		m.TargetArch,
		m.SHA256,
		m.SizeBytes,
		m.BuildTimestamp.Unix(),
	))
}

// Load reads and parses a JSON manifest file from disk.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest json: %w", err)
	}

	return &m, nil
}

// VerifySignature validates that the manifest payload was signed by the trusted Ed25519 public key.
func (m *Manifest) VerifySignature(publicKeyHex string) error {
	pubBytes, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		return fmt.Errorf("invalid public key hex string: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key size: expected %d bytes, got %d", ed25519.PublicKeySize, len(pubBytes))
	}

	sigBytes, err := hex.DecodeString(m.Signature)
	if err != nil {
		return fmt.Errorf("invalid signature hex string: %w", err)
	}
	if len(sigBytes) != ed25519.SignatureSize {
		return fmt.Errorf("invalid signature size: expected %d bytes, got %d", ed25519.SignatureSize, len(sigBytes))
	}

	payload := m.CanonicalPayload()
	if !ed25519.Verify(pubBytes, payload, sigBytes) {
		return errors.New("cryptographic signature verification failed: manifest untrusted or tampered")
	}

	return nil
}

// Sign signs the canonical payload using an Ed25519 private key and writes the hex signature to m.Signature.
func (m *Manifest) Sign(privateKeyHex string) error {
	privBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return fmt.Errorf("invalid private key hex string: %w", err)
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid private key size: expected %d bytes, got %d", ed25519.PrivateKeySize, len(privBytes))
	}

	payload := m.CanonicalPayload()
	sig := ed25519.Sign(privBytes, payload)
	m.Signature = hex.EncodeToString(sig)
	return nil
}

// VerifyFileChecksum calculates the SHA256 of the given file path and compares it with expected.
func VerifyFileChecksum(filePath, expectedSHA256 string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file for checksum: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return fmt.Errorf("error reading file during hashing: %w", err)
	}

	computed := hex.EncodeToString(hasher.Sum(nil))
	if computed != expectedSHA256 {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSHA256, computed)
	}

	return nil
}
