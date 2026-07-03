package cloudauth

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- KDF guards -------------------------------------------------------------

func TestDeriveRunKeyRejectsReservedRunID(t *testing.T) {
	_, err := DeriveRunKey(goldenMaster(t), "account")
	if !errors.Is(err, ErrReservedRunID) {
		t.Fatalf("DeriveRunKey(master, \"account\") = %v, want ErrReservedRunID", err)
	}
}

func TestDeriveRejectsWrongMasterLength(t *testing.T) {
	short := []byte{1, 2, 3}
	if _, _, err := DeriveAccountKeypair(short); err == nil {
		t.Fatal("DeriveAccountKeypair accepted a 3-byte master secret")
	}
	if _, err := DeriveRunKey(short, "run-0001"); err == nil {
		t.Fatal("DeriveRunKey accepted a 3-byte master secret")
	}
}

// --- envelope ---------------------------------------------------------------

func TestEncryptDecryptRoundTrip(t *testing.T) {
	runKey, err := DeriveRunKey(goldenMaster(t), "run-0001")
	if err != nil {
		t.Fatalf("DeriveRunKey: %v", err)
	}
	event := []byte(`{"kind":"message","role":"assistant","text":"round trip"}`)
	blob, err := EncryptEvent(runKey, event)
	if err != nil {
		t.Fatalf("EncryptEvent: %v", err)
	}
	if blob.Alg != AlgXChaCha20Poly1305 {
		t.Fatalf("blob.Alg = %q, want %q", blob.Alg, AlgXChaCha20Poly1305)
	}
	nonce, err := base64.StdEncoding.DecodeString(blob.Nonce)
	if err != nil {
		t.Fatalf("nonce is not standard base64: %v", err)
	}
	if len(nonce) != 24 {
		t.Fatalf("nonce length = %d, want 24", len(nonce))
	}
	got, err := DecryptEvent(runKey, blob)
	if err != nil {
		t.Fatalf("DecryptEvent: %v", err)
	}
	if !bytes.Equal(got, event) {
		t.Fatalf("round trip mismatch:\n got  %s\n want %s", got, event)
	}
}

func TestDecryptEventRejectsTamper(t *testing.T) {
	runKey, err := DeriveRunKey(goldenMaster(t), "run-0001")
	if err != nil {
		t.Fatalf("DeriveRunKey: %v", err)
	}
	blob, err := EncryptEvent(runKey, []byte(`{"kind":"message"}`))
	if err != nil {
		t.Fatalf("EncryptEvent: %v", err)
	}
	ct, err := base64.StdEncoding.DecodeString(blob.Ct)
	if err != nil {
		t.Fatalf("decode ct: %v", err)
	}
	ct[0] ^= 0x01 // flip one bit
	blob.Ct = base64.StdEncoding.EncodeToString(ct)
	if _, err := DecryptEvent(runKey, blob); err == nil {
		t.Fatal("DecryptEvent accepted a tampered ciphertext")
	}
}

func TestDecryptEventRejectsWrongKey(t *testing.T) {
	master := goldenMaster(t)
	keyA, _ := DeriveRunKey(master, "run-0001")
	keyB, _ := DeriveRunKey(master, "run-0002")
	blob, err := EncryptEvent(keyA, []byte(`{"kind":"message"}`))
	if err != nil {
		t.Fatalf("EncryptEvent: %v", err)
	}
	if _, err := DecryptEvent(keyB, blob); err == nil {
		t.Fatal("DecryptEvent accepted a ciphertext under the wrong key")
	}
}

func TestDecryptEventRejectsUnknownAlg(t *testing.T) {
	runKey, _ := DeriveRunKey(goldenMaster(t), "run-0001")
	blob, err := EncryptEvent(runKey, []byte(`{}`))
	if err != nil {
		t.Fatalf("EncryptEvent: %v", err)
	}
	blob.Alg = "aes-gcm"
	if _, err := DecryptEvent(runKey, blob); err == nil {
		t.Fatal("DecryptEvent accepted an unknown alg")
	}
}

// --- token ------------------------------------------------------------------

// Pinned vector, computed independently (Python hmac/hashlib/base64) for
// secret "relay-secret", payload {"accountId":"acct-1","exp":1700000000000}.
// Pins the exact JSON encoding + base64urlnopad of the TS token format.
const pinnedToken = "eyJhY2NvdW50SWQiOiJhY2N0LTEiLCJleHAiOjE3MDAwMDAwMDAwMDB9." +
	"6VfEvLhVp5XZ63MNLA0YikYK0RrMva0jY1MpxSAeUnE"

func TestIssueTokenPinnedVector(t *testing.T) {
	got := IssueToken("relay-secret", "acct-1", 1_700_000_000_000, 0)
	if got != pinnedToken {
		t.Fatalf("IssueToken mismatch:\n got  %s\n want %s", got, pinnedToken)
	}
}

func TestVerifyTokenRoundTrip(t *testing.T) {
	const secret = "relay-secret"
	now := int64(1_750_000_000_000)
	token := IssueToken(secret, "acct-42", 24*60*60*1000, now)

	accountID, ok := VerifyToken(secret, token, now)
	if !ok || accountID != "acct-42" {
		t.Fatalf("VerifyToken = (%q, %v), want (\"acct-42\", true)", accountID, ok)
	}
	// TS-format parity: the pinned vector verifies too.
	accountID, ok = VerifyToken(secret, pinnedToken, 1_600_000_000_000)
	if !ok || accountID != "acct-1" {
		t.Fatalf("VerifyToken(pinned) = (%q, %v), want (\"acct-1\", true)", accountID, ok)
	}
}

func TestVerifyTokenRejects(t *testing.T) {
	const secret = "relay-secret"
	now := int64(1_750_000_000_000)
	token := IssueToken(secret, "acct-42", 1000, now)

	if _, ok := VerifyToken(secret, token, now+1001); ok {
		t.Fatal("VerifyToken accepted an expired token")
	}
	if _, ok := VerifyToken("wrong-secret", token, now); ok {
		t.Fatal("VerifyToken accepted a token under the wrong secret")
	}
	if _, ok := VerifyToken(secret, "no-dot-here", now); ok {
		t.Fatal("VerifyToken accepted a token with no separator")
	}
	if _, ok := VerifyToken(secret, token+"x", now); ok {
		t.Fatal("VerifyToken accepted a token with a mangled mac")
	}
	body, _, _ := strings.Cut(token, ".")
	if _, ok := VerifyToken(secret, "x"+body+"."+macOf(secret, body), now); ok {
		t.Fatal("VerifyToken accepted a token with a mangled body")
	}
}

// --- LoadMasterSecret -------------------------------------------------------

const (
	hexA = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	hexB = "202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"
	hexC = "404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"
	hexD = "606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f"
)

// fakeHome points HOME (and USERPROFILE, for portability) at a temp dir so
// LoadMasterSecret never touches the real ~/.config, and clears both env vars.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PACTIFY_MASTER_SECRET", "")
	t.Setenv("LINX_MASTER_SECRET", "")
	return home
}

func writeSecretFile(t *testing.T, home, app, content string) {
	t.Helper()
	dir := filepath.Join(home, ".config", app)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "master-secret"), []byte(content), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
}

func TestLoadMasterSecretPrecedence(t *testing.T) {
	home := fakeHome(t)
	writeSecretFile(t, home, "pactify", hexC+"\n")
	writeSecretFile(t, home, "linx", hexD+"\n")

	// 1. PACTIFY env wins over everything.
	t.Setenv("PACTIFY_MASTER_SECRET", hexA)
	t.Setenv("LINX_MASTER_SECRET", hexB)
	got, err := LoadMasterSecret()
	if err != nil {
		t.Fatalf("LoadMasterSecret: %v", err)
	}
	if want, _ := decodeMasterSecret(hexA, "test"); !bytes.Equal(got, want) {
		t.Fatal("expected $PACTIFY_MASTER_SECRET to win")
	}

	// 2. LINX env when PACTIFY unset.
	t.Setenv("PACTIFY_MASTER_SECRET", "")
	got, err = LoadMasterSecret()
	if err != nil {
		t.Fatalf("LoadMasterSecret: %v", err)
	}
	if want, _ := decodeMasterSecret(hexB, "test"); !bytes.Equal(got, want) {
		t.Fatal("expected $LINX_MASTER_SECRET to win over files")
	}

	// 3. pactify file when both envs unset (trailing newline tolerated).
	t.Setenv("LINX_MASTER_SECRET", "")
	got, err = LoadMasterSecret()
	if err != nil {
		t.Fatalf("LoadMasterSecret: %v", err)
	}
	if want, _ := decodeMasterSecret(hexC, "test"); !bytes.Equal(got, want) {
		t.Fatal("expected ~/.config/pactify/master-secret to win over linx file")
	}

	// 4. linx file as last fallback.
	if err := os.Remove(filepath.Join(home, ".config", "pactify", "master-secret")); err != nil {
		t.Fatalf("remove pactify file: %v", err)
	}
	got, err = LoadMasterSecret()
	if err != nil {
		t.Fatalf("LoadMasterSecret: %v", err)
	}
	if want, _ := decodeMasterSecret(hexD, "test"); !bytes.Equal(got, want) {
		t.Fatal("expected ~/.config/linx/master-secret fallback")
	}
}

func TestLoadMasterSecretErrors(t *testing.T) {
	home := fakeHome(t)

	if _, err := LoadMasterSecret(); !errors.Is(err, ErrNoMasterSecret) {
		t.Fatalf("no sources: err = %v, want ErrNoMasterSecret", err)
	}

	t.Setenv("PACTIFY_MASTER_SECRET", "not-hex")
	if _, err := LoadMasterSecret(); err == nil {
		t.Fatal("accepted a non-hex env secret")
	}

	t.Setenv("PACTIFY_MASTER_SECRET", "0011") // valid hex, wrong length
	if _, err := LoadMasterSecret(); err == nil {
		t.Fatal("accepted a 2-byte env secret")
	}

	// A corrupt file surfaces as an error, not a fall-through to linx.
	t.Setenv("PACTIFY_MASTER_SECRET", "")
	writeSecretFile(t, home, "pactify", "corrupt")
	writeSecretFile(t, home, "linx", hexD)
	if _, err := LoadMasterSecret(); err == nil {
		t.Fatal("a corrupt pactify file should error, not fall through")
	}
}
