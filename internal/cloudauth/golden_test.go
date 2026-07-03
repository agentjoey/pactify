// Golden vectors — the cross-language compatibility contract.
//
// These pin the EXACT byte outputs of the account/run key derivations and the
// xchacha20poly1305 envelope for fixed inputs, transcribed verbatim from
// docs/specs/agentworks-wire.md §8. The TypeScript reference test is
// cloud/crypto/test/golden.test.ts. A mismatch means account identity and E2E
// decryption silently break across languages.
//
// DO NOT edit an expected value to make a failing test pass. A mismatch means
// the derivation/encryption changed, which is a cross-language BREAK: either
// revert the change, or bump the protocol version and regenerate vectors on
// BOTH sides deliberately.
package cloudauth

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
)

// Fixed master secret: bytes 0x00..0x1f (§8).
const goldenMasterHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func goldenMaster(t *testing.T) []byte {
	t.Helper()
	master, err := hex.DecodeString(goldenMasterHex)
	if err != nil {
		t.Fatalf("decode master secret: %v", err)
	}
	return master
}

func TestGoldenAccountPublicKey(t *testing.T) {
	pubHex, _, err := DeriveAccountKeypair(goldenMaster(t))
	if err != nil {
		t.Fatalf("DeriveAccountKeypair: %v", err)
	}
	const want = "ca14d356f48c1391eb7c8b51970f768360e9d25e5d9fded81b55d6aef64d79b7"
	if pubHex != want {
		t.Fatalf("account publicKeyHex mismatch:\n got  %s\n want %s", pubHex, want)
	}
}

func TestGoldenAccountSignature(t *testing.T) {
	_, sign, err := DeriveAccountKeypair(goldenMaster(t))
	if err != nil {
		t.Fatalf("DeriveAccountKeypair: %v", err)
	}
	got := sign("pactify-golden-challenge")
	const want = "d8eb8cab4286a9062f09d6c1c9e539de865cf6883e080234545735936375f2d1" +
		"5335a256a5d098bf67fe9aad98c5b1dac2225fcc697c4bfeaac1ed29d06afe08"
	if got != want {
		t.Fatalf("sign(\"pactify-golden-challenge\") mismatch:\n got  %s\n want %s", got, want)
	}
}

func TestGoldenProjectKey(t *testing.T) {
	// Vector computed independently via the TS @noble reference (HKDF-SHA256,
	// nil salt, info "project:acct1:pactify") — proves the U2 project-key
	// derivation interoperates byte-for-byte with the future TS decrypt in the
	// Mission Control frontend.
	key, err := DeriveProjectKey(goldenMaster(t), "acct1:pactify")
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(key); got != "cb1824a13ab023fe2af7238df9dd1e2a5d53a9abf01d1bf446b4725840ddfdd7" {
		t.Fatalf("project key %s, want cb18…fdd7", got)
	}
}

func TestGoldenRunKey(t *testing.T) {
	runKey, err := DeriveRunKey(goldenMaster(t), "run-0001")
	if err != nil {
		t.Fatalf("DeriveRunKey: %v", err)
	}
	const want = "a58393253ee3c085d948bb7f28af35887f835d95671f61cbd8aee617f21edac9"
	if got := hex.EncodeToString(runKey); got != want {
		t.Fatalf("deriveRunKey(master, \"run-0001\") mismatch:\n got  %s\n want %s", got, want)
	}
}

// Envelope vector (§8): fixed key + nonce + plaintext → fixed ciphertext.
const (
	goldenEnvKeyHex    = "01080f161d242b323940474e555c636a71787f868d949ba2a9b0b7bec5ccd3da"
	goldenEnvNonceB64  = "AgUICw4RFBcaHSAjJiksLzI1ODs+QURH"
	goldenEnvPlaintext = `{"kind":"message","role":"assistant","text":"golden"}`
	goldenEnvCtB64     = "8fzdY0Aj4+cQ775q7fHllfmR+blpRft8Dg6V/XY6WqhTq2Px0H/V/ToElSTRrZxL3dWTUs+vkLP9SDD06QA1YCl21UnF"
)

func TestGoldenEnvelopeSeal(t *testing.T) {
	// encryptEvent uses a random nonce, so pin the underlying AEAD directly
	// via the internal seal helper with the fixed key+nonce from §8.
	key, err := hex.DecodeString(goldenEnvKeyHex)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(goldenEnvNonceB64)
	if err != nil {
		t.Fatalf("decode nonce: %v", err)
	}
	ct, err := seal(key, nonce, []byte(goldenEnvPlaintext))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if got := base64.StdEncoding.EncodeToString(ct); got != goldenEnvCtB64 {
		t.Fatalf("envelope ciphertext mismatch:\n got  %s\n want %s", got, goldenEnvCtB64)
	}
}

func TestGoldenDecryptEvent(t *testing.T) {
	key, err := hex.DecodeString(goldenEnvKeyHex)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	blob := EncryptedBlob{
		Alg:   "xchacha20poly1305",
		Nonce: goldenEnvNonceB64,
		Ct:    goldenEnvCtB64,
	}
	pt, err := DecryptEvent(key, blob)
	if err != nil {
		t.Fatalf("DecryptEvent: %v", err)
	}
	if string(pt) != goldenEnvPlaintext {
		t.Fatalf("plaintext mismatch:\n got  %s\n want %s", pt, goldenEnvPlaintext)
	}
}
