package cloudclient

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentjoey/pactify/internal/cloudauth"
)

// mockRelay stands in for the real relay's POST /v1/auth: it verifies the
// Ed25519 signature over the challenge (exactly as cloud/relay/src/auth.ts
// verifyChallenge does) and issues an HMAC token via cloudauth.IssueToken.
func mockRelay(t *testing.T, secret string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body authRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
			body.PublicKey == "" || body.Challenge == "" || body.Signature == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad request"}`))
			return
		}
		pub, err1 := hex.DecodeString(body.PublicKey)
		sig, err2 := hex.DecodeString(body.Signature)
		if err1 != nil || err2 != nil || len(pub) != ed25519.PublicKeySize ||
			!ed25519.Verify(pub, []byte(body.Challenge), sig) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		// Account id derived deterministically from the pubkey (the real relay
		// upserts an Account keyed by publicKey; the id shape is irrelevant here).
		accountID := "acct_" + body.PublicKey[:12]
		token := cloudauth.IssueToken(secret, accountID, 24*3600*1000, 1_700_000_000_000)
		_ = json.NewEncoder(w).Encode(authResponse{Token: token, AccountID: accountID})
	}))
}

func TestAuthenticateSuccess(t *testing.T) {
	const relaySecret = "test-relay-secret-at-least-32-bytes-long!!"
	srv := mockRelay(t, relaySecret)
	defer srv.Close()

	master := make([]byte, 32)
	for i := range master {
		master[i] = byte(i)
	}
	sess, err := New(srv.URL).Authenticate(context.Background(), master)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	// The pubkey must be the golden account pubkey for this master secret.
	if sess.PublicKeyHex != "ca14d356f48c1391eb7c8b51970f768360e9d25e5d9fded81b55d6aef64d79b7" {
		t.Fatalf("unexpected pubkey %q", sess.PublicKeyHex)
	}
	if sess.AccountID == "" || sess.Token == "" {
		t.Fatalf("empty session fields: %+v", sess)
	}
	// The issued token must verify under the relay secret.
	if acct, ok := cloudauth.VerifyToken(relaySecret, sess.Token, 1_700_000_000_001); !ok || acct != sess.AccountID {
		t.Fatalf("issued token does not verify: ok=%v acct=%q want=%q", ok, acct, sess.AccountID)
	}
}

func TestAuthenticateRejectsBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL).Authenticate(context.Background(), make([]byte, 32)); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestSessionSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	want := &Session{RelayURL: "https://r", AccountID: "acct_x", PublicKeyHex: "ab", Token: "b.c", ObtainedAtMs: 42}
	if err := SaveSession(want); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	got, err := LoadSession()
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if *got != *want {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, want)
	}
}

func TestTokenExpMs(t *testing.T) {
	tok := cloudauth.IssueToken("s", "acct", 1000, 5000)
	exp, ok := TokenExpMs(tok)
	if !ok || exp != 6000 {
		t.Fatalf("TokenExpMs = %d,%v want 6000,true", exp, ok)
	}
	if _, ok := TokenExpMs("garbage"); ok {
		t.Fatal("garbage token should not parse")
	}
}
