package cloudauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
)

// tokenPayload mirrors the relay's TokenPayload (cloud/relay/src/auth.ts).
// Field order matches JSON.stringify({accountId, exp}) so Go-issued tokens are
// byte-identical to TS-issued ones for the same inputs.
type tokenPayload struct {
	AccountID string `json:"accountId"`
	Exp       int64  `json:"exp"`
}

// macOf computes base64urlnopad(HMAC-SHA256(secret, body)) per spec §6.
func macOf(secret, body string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// IssueToken issues a stateless bearer token (spec §6, FROZEN v1 format):
//
//	base64urlnopad(JSON{accountId, exp}) + "." + base64urlnopad(HMAC-SHA256(secret, body))
//
// Token issuance in production is relay-side (TypeScript); this exists for
// parity and round-trip testing on the Go side.
func IssueToken(secret, accountID string, ttlMs, nowUnixMs int64) string {
	payload, err := json.Marshal(tokenPayload{AccountID: accountID, Exp: nowUnixMs + ttlMs})
	if err != nil {
		// Marshalling a string+int64 struct cannot fail.
		panic("cloudauth: marshal token payload: " + err.Error())
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + macOf(secret, body)
}

// VerifyToken verifies a bearer token (spec §6): constant-time HMAC compare,
// then payload decode and expiry check (exp < now rejects). It returns the
// accountId and true on success, or ("", false) on bad MAC / expired /
// malformed input.
func VerifyToken(secret, token string, nowUnixMs int64) (accountID string, ok bool) {
	body, sig, found := strings.Cut(token, ".")
	if !found {
		return "", false
	}
	// hmac.Equal is constant-time (crypto/subtle); length mismatch rejects
	// without leaking via early-out on content.
	if !hmac.Equal([]byte(sig), []byte(macOf(secret, body))) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return "", false
	}
	var payload tokenPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", false
	}
	if payload.AccountID == "" || payload.Exp < nowUnixMs {
		return "", false
	}
	return payload.AccountID, true
}
