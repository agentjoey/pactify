package cloudclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/cloudauth"
)

// pairPollInterval is how often Pair polls the relay for completion.
var pairPollInterval = 2 * time.Second

type pairInitResp struct {
	Code string `json:"code"`
}

type pairStatus struct {
	State   string `json:"state"`
	Payload *struct {
		EpkMachine string `json:"epkMachine"`
		Ciphertext string `json:"ciphertext"`
	} `json:"payload"`
}

// Pair runs the provision-mode, relay-blind pairing handshake as the secret-less
// receiver (docs/specs/agentworks-wire.md §7): open a session, publish this
// machine's ephemeral X25519 public key, then poll until an already-authenticated
// device wraps the master secret back. onCode is called once with the pairing
// code so the caller can show the user what to approve on their paired device.
// Returns the decrypted 32-byte master secret. The relay only ever couriers
// public keys + ciphertext — it never sees the secret.
func (c *Client) Pair(ctx context.Context, onCode func(code string)) ([]byte, error) {
	// 1. Open a provision session.
	var initR pairInitResp
	if err := c.doJSON(ctx, http.MethodPost, "/v1/pair/init", map[string]string{"mode": "provision"}, &initR); err != nil {
		return nil, fmt.Errorf("open pairing session: %w", err)
	}
	if initR.Code == "" {
		return nil, fmt.Errorf("relay returned an empty pairing code")
	}

	// 2. Generate our ephemeral keypair and publish the public key.
	pubHex, priv, err := cloudauth.NewPairingEphemeral()
	if err != nil {
		return nil, err
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/pair/"+initR.Code+"/ready", map[string]string{"epkMachine": pubHex}, nil); err != nil {
		return nil, fmt.Errorf("publish ephemeral key: %w", err)
	}

	// 3. Tell the user which code to approve.
	if onCode != nil {
		onCode(initR.Code)
	}

	// 4. Poll until the holder completes the wrap (or ctx deadline / expiry).
	ticker := time.NewTicker(pairPollInterval)
	defer ticker.Stop()
	for {
		var st pairStatus
		err := c.doJSON(ctx, http.MethodGet, "/v1/pair/"+initR.Code+"/status", nil, &st)
		if err != nil {
			if strings.Contains(err.Error(), "HTTP 410") {
				return nil, fmt.Errorf("pairing session expired before it was approved")
			}
			return nil, fmt.Errorf("poll pairing status: %w", err)
		}
		if st.State == "completed" && st.Payload != nil {
			secret, err := cloudauth.PairingComplete(priv, st.Payload.EpkMachine, st.Payload.Ciphertext)
			if err != nil {
				return nil, err
			}
			return secret, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("pairing timed out before approval: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// doJSON performs an HTTP request with an optional JSON body and decodes an
// optional JSON response. Non-2xx is an error carrying the status.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
