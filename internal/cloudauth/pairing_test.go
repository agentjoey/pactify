package cloudauth

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// Cross-language golden vector: the ciphertext was produced by the TypeScript
// reference (cloud/crypto, @noble) with a fixed receiver ephemeral private key,
// a fixed holder ephemeral key, and a fixed nonce. Go's PairingComplete must
// recover the exact master secret — proving X25519 + HKDF("linx-pair-v1") +
// XChaCha20-Poly1305 interoperate byte-for-byte with the TS handshake.
func TestGoldenPairingComplete(t *testing.T) {
	recvPriv, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	holderPubHex := "605a725d2a4adfeeb1a29e17edd621c1b7593ee8cdbc44ac6c4ab6e2f805d23c"
	wire := "AwgNEhccISYrMDU6P0RJTlNYXWJnbHF2R3N3GdT3PH3cHfEL1NtnsQ7bFMV_1MJDVF7JHWvYxXwTTt6lTor0fzR8e0sIj84-"
	want := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

	secret, err := PairingComplete(recvPriv, holderPubHex, wire)
	if err != nil {
		t.Fatalf("PairingComplete: %v", err)
	}
	if got := hex.EncodeToString(secret); got != want {
		t.Fatalf("recovered secret %s, want %s", got, want)
	}
}

func TestPairingRoundTrip(t *testing.T) {
	master := make([]byte, 32)
	for i := range master {
		master[i] = byte(i * 3)
	}
	// Receiver generates its ephemeral keypair; holder wraps the secret for it.
	recvPubHex, recvPriv, err := NewPairingEphemeral()
	if err != nil {
		t.Fatal(err)
	}
	holderPubHex, wire, err := PairingRespond(master, recvPubHex)
	if err != nil {
		t.Fatal(err)
	}
	got, err := PairingComplete(recvPriv, holderPubHex, wire)
	if err != nil {
		t.Fatalf("PairingComplete: %v", err)
	}
	if !bytes.Equal(got, master) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestPairingCompleteRejectsTamper(t *testing.T) {
	master := make([]byte, 32)
	recvPubHex, recvPriv, _ := NewPairingEphemeral()
	holderPubHex, wire, _ := PairingRespond(master, recvPubHex)
	// Flip a byte in the middle of the wire ciphertext.
	b := []byte(wire)
	b[len(b)/2] ^= 0x01
	if _, err := PairingComplete(recvPriv, holderPubHex, string(b)); err == nil {
		t.Fatal("expected decrypt failure on tampered ciphertext")
	}
	// Wrong receiver key → wrong shared secret → AEAD fails.
	_, otherPriv, _ := NewPairingEphemeral()
	if _, err := PairingComplete(otherPriv, holderPubHex, wire); err == nil {
		t.Fatal("expected decrypt failure with wrong receiver key")
	}
}
