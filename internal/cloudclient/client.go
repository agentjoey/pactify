// Package cloudclient is the pactify (machine-side) HTTP client for the shared
// AgentWorks relay. It turns a master secret into an authenticated session by
// deriving the account keypair (internal/cloudauth), signing a challenge, and
// exchanging it for a bearer token at POST /v1/auth. Protocol: docs/specs/
// agentworks-wire.md §6.
package cloudclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/cloudauth"
)

// Session is a cached authenticated session with a relay.
type Session struct {
	RelayURL     string `json:"relay_url"`
	AccountID    string `json:"account_id"`
	PublicKeyHex string `json:"public_key_hex"`
	Token        string `json:"token"`
	ObtainedAtMs int64  `json:"obtained_at_ms"`
}

// Client talks to one relay base URL.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New builds a client for baseURL (trailing slash trimmed).
func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

// RelayURLFromEnv returns the configured relay base URL ($PACT_RELAY_URL), or "".
func RelayURLFromEnv() string { return os.Getenv("PACT_RELAY_URL") }

type authRequest struct {
	PublicKey string `json:"publicKey"`
	Challenge string `json:"challenge"`
	Signature string `json:"signature"`
}

type authResponse struct {
	Token     string `json:"token"`
	AccountID string `json:"accountId"`
}

// Authenticate derives the account keypair from masterSecret, signs a fresh
// challenge, and exchanges it for a bearer token at POST /v1/auth. The challenge
// is client-generated (v1: the relay does not yet issue server nonces — see spec
// §6.1); a random 32-byte value avoids any accidental reuse.
func (c *Client) Authenticate(ctx context.Context, masterSecret []byte) (*Session, error) {
	pubHex, sign, err := cloudauth.DeriveAccountKeypair(masterSecret)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate challenge: %w", err)
	}
	challenge := hex.EncodeToString(nonce)

	body, _ := json.Marshal(authRequest{PublicKey: pubHex, Challenge: challenge, Signature: sign(challenge)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/auth", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST /v1/auth: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay auth failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var ar authResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return nil, fmt.Errorf("decode auth response: %w", err)
	}
	if ar.Token == "" || ar.AccountID == "" {
		return nil, fmt.Errorf("relay returned an incomplete auth response")
	}
	return &Session{
		RelayURL:     c.BaseURL,
		AccountID:    ar.AccountID,
		PublicKeyHex: pubHex,
		Token:        ar.Token,
		ObtainedAtMs: time.Now().UnixMilli(),
	}, nil
}

// SessionPath is where the cached session lives (~/.config/pactify/session.json).
func SessionPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "pactify", "session.json"), nil
}

// SaveSession writes s to SessionPath() with 0600 perms (contains a bearer token).
func SaveSession(s *Session) error {
	p, err := SessionPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// LoadSession reads the cached session, or an error if none exists.
func LoadSession() (*Session, error) {
	p, err := SessionPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// TokenExpMs decodes a bearer token's `exp` (epoch ms) without verifying the MAC.
// Safe for local display of a token we already hold. Returns false if unparseable.
func TokenExpMs(token string) (int64, bool) {
	dot := strings.IndexByte(token, '.')
	if dot < 0 {
		return 0, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(token[:dot])
	if err != nil {
		return 0, false
	}
	var p struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return 0, false
	}
	return p.Exp, true
}
