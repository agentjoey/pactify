// Package cloudauth is the Go port of the AgentWorks account-layer
// cryptography specified in docs/specs/agentworks-wire.md (FROZEN v1).
//
// The TypeScript reference lives in cloud/crypto; the golden vectors in
// golden_test.go (spec §8) pin byte-exact cross-language compatibility with
// existing Linx production accounts. Any change that alters those outputs is a
// protocol break and must go through a version bump — never land in place.
package cloudauth

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// masterSecretLen is the size of the account master secret in bytes.
const masterSecretLen = 32

// keyLen is the size of every derived key (HKDF output length L) in bytes.
const keyLen = 32

// hkdf32 derives a 32-byte key from the master secret per spec §4:
// HKDF-SHA256 with a nil salt (RFC 5869 zero-filled salt of HashLen) and the
// given info bytes. The salt MUST be nil, not []byte{} (spec §4.1).
func hkdf32(masterSecret, info []byte) ([]byte, error) {
	out := make([]byte, keyLen)
	if _, err := io.ReadFull(hkdf.New(sha256.New, masterSecret, nil, info), out); err != nil {
		return nil, fmt.Errorf("cloudauth: hkdf: %w", err)
	}
	return out, nil
}

func checkMasterSecret(masterSecret []byte) error {
	if len(masterSecret) != masterSecretLen {
		return fmt.Errorf("cloudauth: master secret must be %d bytes, got %d", masterSecretLen, len(masterSecret))
	}
	return nil
}

// DeriveAccountKeypair derives the account Ed25519 keypair from the master
// secret (spec §4.1, FROZEN v1):
//
//	seed = HKDF-SHA256(ikm=masterSecret, salt=nil, info="account", L=32)
//
// It returns the lowercase-hex public key (the canonical account id material)
// and a sign function that returns the lowercase-hex Ed25519 signature over
// the UTF-8 bytes of the challenge. Both the machine and the web derive the
// same keypair from the same master secret → the same account.
func DeriveAccountKeypair(masterSecret []byte) (publicKeyHex string, sign func(challenge string) string, err error) {
	if err := checkMasterSecret(masterSecret); err != nil {
		return "", nil, err
	}
	seed, err := hkdf32(masterSecret, []byte("account"))
	if err != nil {
		return "", nil, err
	}
	priv := ed25519.NewKeyFromSeed(seed)
	publicKeyHex = hex.EncodeToString(priv.Public().(ed25519.PublicKey))
	sign = func(challenge string) string {
		return hex.EncodeToString(ed25519.Sign(priv, []byte(challenge)))
	}
	return publicKeyHex, sign, nil
}

// ErrReservedRunID is returned by DeriveRunKey for runID == "account": with
// v1's zero-salt KDF that runID would collide with the account seed
// (spec §4.3, non-breaking guard).
var ErrReservedRunID = errors.New(`cloudauth: runID "account" is reserved`)

// DeriveRunKey derives the per-run symmetric key (spec §4.2, FROZEN v1):
//
//	runKey = HKDF-SHA256(ikm=masterSecret, salt=nil, info="run:"+runID, L=32)
//
// The relay never holds the master secret, so it cannot derive this; any
// device with the master secret plus the cleartext runID can.
func DeriveRunKey(masterSecret []byte, runID string) ([]byte, error) {
	if err := checkMasterSecret(masterSecret); err != nil {
		return nil, err
	}
	if runID == "account" {
		return nil, ErrReservedRunID
	}
	return hkdf32(masterSecret, []byte("run:"+runID))
}

// DeriveProjectKey derives the per-project symmetric key used to E2E-encrypt the
// body of U2 Mission Control pact events, from the master secret + the cleartext
// projectID via HKDF-SHA256 with info "project:<projectID>". Domain-separated
// from run keys ("run:") and the account seed ("account"), so a project key can
// never coincide with either. The relay never holds the master secret, so it
// cannot derive this; any device with the master secret + the cleartext
// projectID can (keyless distribution).
func DeriveProjectKey(masterSecret []byte, projectID string) ([]byte, error) {
	if err := checkMasterSecret(masterSecret); err != nil {
		return nil, err
	}
	return hkdf32(masterSecret, []byte("project:"+projectID))
}
