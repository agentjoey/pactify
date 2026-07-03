package cloudauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// Relay-blind pairing via ephemeral X25519 ECDH (docs/specs/agentworks-wire.md §7,
// cloud/crypto/src/pairing.ts). Both sides derive the same key from the ECDH
// shared secret via HKDF-SHA256 with info "linx-pair-v1" (FROZEN v1 — must match
// the TypeScript reference byte-for-byte) and wrap the master secret with
// XChaCha20-Poly1305. Wire ciphertext = base64url(nonce(24) || ct).
const pairInfo = "linx-pair-v1"

// pairingKey derives the symmetric pairing key from an X25519 shared secret.
func pairingKey(shared []byte) ([]byte, error) {
	r := hkdf.New(sha256.New, shared, nil, []byte(pairInfo))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}

// decodePairB64url decodes the wire ciphertext. @scure/base `base64url` and Go
// agree byte-for-byte for a wrapped 32-byte secret (72 bytes → no padding); we
// strip any padding to be tolerant either way.
func decodePairB64url(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// NewPairingEphemeral generates an X25519 ephemeral keypair, returning the public
// key as lowercase hex plus the raw 32-byte private key.
func NewPairingEphemeral() (pubHex string, priv []byte, err error) {
	priv = make([]byte, 32)
	if _, err = rand.Read(priv); err != nil {
		return "", nil, err
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return "", nil, err
	}
	return hex.EncodeToString(pub), priv, nil
}

// PairingComplete is the receiver side (a secret-less machine): derive the shared
// key from the holder's ephemeral public key and decrypt the wrapped master
// secret. Errors on a bad key/tamper (AEAD tag) or malformed input.
func PairingComplete(priv []byte, holderPubHex, ciphertextB64url string) ([]byte, error) {
	holderPub, err := hex.DecodeString(holderPubHex)
	if err != nil {
		return nil, fmt.Errorf("decode holder pubkey: %w", err)
	}
	shared, err := curve25519.X25519(priv, holderPub)
	if err != nil {
		return nil, fmt.Errorf("x25519: %w", err)
	}
	key, err := pairingKey(shared)
	if err != nil {
		return nil, err
	}
	raw, err := decodePairB64url(ciphertextB64url)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(raw) < chacha20poly1305.NonceSizeX {
		return nil, fmt.Errorf("pairing ciphertext too short")
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	pt, err := aead.Open(nil, raw[:chacha20poly1305.NonceSizeX], raw[chacha20poly1305.NonceSizeX:], nil)
	if err != nil {
		return nil, fmt.Errorf("pairing decrypt failed (tamper/wrong key): %w", err)
	}
	return pt, nil
}

// PairingRespond is the holder side (has the secret): generate an ephemeral
// keypair, derive the shared key from the receiver's ephemeral public key, and
// wrap masterSecret. Returns the holder's ephemeral public key (hex) and
// base64url(nonce(24) || ct). Primarily for tests and CLI symmetry — in the
// unified system the authenticated web/linx performs this step.
func PairingRespond(masterSecret []byte, receiverPubHex string) (pubHex, ciphertextB64url string, err error) {
	receiverPub, err := hex.DecodeString(receiverPubHex)
	if err != nil {
		return "", "", fmt.Errorf("decode receiver pubkey: %w", err)
	}
	priv := make([]byte, 32)
	if _, err = rand.Read(priv); err != nil {
		return "", "", err
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return "", "", err
	}
	shared, err := curve25519.X25519(priv, receiverPub)
	if err != nil {
		return "", "", fmt.Errorf("x25519: %w", err)
	}
	key, err := pairingKey(shared)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err = rand.Read(nonce); err != nil {
		return "", "", err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", "", err
	}
	ct := aead.Seal(nil, nonce, masterSecret, nil)
	wire := append(append([]byte{}, nonce...), ct...)
	return hex.EncodeToString(pub), base64.RawURLEncoding.EncodeToString(wire), nil
}
