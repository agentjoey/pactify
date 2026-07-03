package cloudauth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// AlgXChaCha20Poly1305 is the only envelope algorithm in FROZEN v1 (spec §2.3).
const AlgXChaCha20Poly1305 = "xchacha20poly1305"

// EncryptedBlob is the opaque E2E envelope carried in a WireMessage body
// (spec §2.3, FROZEN v1). Nonce and Ct are STANDARD base64 (with padding),
// matching @scure/base base64 on the TypeScript side. JSON tags match
// cloud/wire EncryptedBlob.
type EncryptedBlob struct {
	Alg   string `json:"alg"`
	Nonce string `json:"nonce"`
	Ct    string `json:"ct"`
}

// seal is the deterministic AEAD core of EncryptEvent, split out so the §8
// golden vector can pin it with a fixed nonce. ct includes the appended
// Poly1305 tag.
func seal(runKey, nonce, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(runKey)
	if err != nil {
		return nil, fmt.Errorf("cloudauth: %w", err)
	}
	return aead.Seal(nil, nonce, plaintext, nil), nil
}

// EncryptEvent encrypts an event's canonical JSON into the wire EncryptedBlob
// envelope (spec §5, FROZEN v1): XChaCha20-Poly1305 with a random 24-byte
// nonce, standard base64 for nonce and ct.
func EncryptEvent(runKey []byte, eventJSON []byte) (EncryptedBlob, error) {
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return EncryptedBlob{}, fmt.Errorf("cloudauth: nonce: %w", err)
	}
	ct, err := seal(runKey, nonce, eventJSON)
	if err != nil {
		return EncryptedBlob{}, err
	}
	return EncryptedBlob{
		Alg:   AlgXChaCha20Poly1305,
		Nonce: base64.StdEncoding.EncodeToString(nonce),
		Ct:    base64.StdEncoding.EncodeToString(ct),
	}, nil
}

// DecryptEvent opens an EncryptedBlob back into the event JSON bytes
// (spec §5). It errors on an unknown alg, malformed base64, a wrong-size
// nonce, or AEAD authentication failure (wrong key / tampering).
func DecryptEvent(runKey []byte, blob EncryptedBlob) ([]byte, error) {
	if blob.Alg != AlgXChaCha20Poly1305 {
		return nil, fmt.Errorf("cloudauth: unsupported alg %q", blob.Alg)
	}
	nonce, err := base64.StdEncoding.DecodeString(blob.Nonce)
	if err != nil {
		return nil, fmt.Errorf("cloudauth: decode nonce: %w", err)
	}
	if len(nonce) != chacha20poly1305.NonceSizeX {
		return nil, fmt.Errorf("cloudauth: nonce must be %d bytes, got %d", chacha20poly1305.NonceSizeX, len(nonce))
	}
	ct, err := base64.StdEncoding.DecodeString(blob.Ct)
	if err != nil {
		return nil, fmt.Errorf("cloudauth: decode ct: %w", err)
	}
	aead, err := chacha20poly1305.NewX(runKey)
	if err != nil {
		return nil, fmt.Errorf("cloudauth: %w", err)
	}
	plaintext, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("cloudauth: decrypt: %w", err)
	}
	return plaintext, nil
}
