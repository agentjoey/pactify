package cloudclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cloudauth"
)

// pairMockRelay implements the provision pairing state machine and plays the
// holder: once the machine publishes its ephemeral key via /ready, it wraps
// `secret` with cloudauth.PairingRespond so /status returns a completed payload.
func pairMockRelay(t *testing.T, secret []byte) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	var recvPub string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/pair/init", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "ABCD1234"})
	})
	mux.HandleFunc("/v1/pair/ABCD1234/ready", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			EpkMachine string `json:"epkMachine"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		recvPub = b.EpkMachine
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1/pair/ABCD1234/status", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		rp := recvPub
		mu.Unlock()
		if rp == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "pending"})
			return
		}
		holderPub, wire, err := cloudauth.PairingRespond(secret, rp)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"state":   "completed",
			"payload": map[string]string{"epkMachine": holderPub, "ciphertext": wire},
		})
	})
	return httptest.NewServer(mux)
}

func TestPairRecoversSecret(t *testing.T) {
	old := pairPollInterval
	pairPollInterval = 5 * time.Millisecond
	defer func() { pairPollInterval = old }()

	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 7)
	}
	srv := pairMockRelay(t, secret)
	defer srv.Close()

	var gotCode string
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	recovered, err := New(srv.URL).Pair(ctx, func(code string) { gotCode = code })
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if gotCode != "ABCD1234" {
		t.Fatalf("onCode got %q", gotCode)
	}
	if hex.EncodeToString(recovered) != hex.EncodeToString(secret) {
		t.Fatalf("recovered secret mismatch")
	}
}

func TestPairTimesOut(t *testing.T) {
	old := pairPollInterval
	pairPollInterval = 5 * time.Millisecond
	defer func() { pairPollInterval = old }()

	// Relay that never completes (no /ready effect on status).
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/pair/init", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "X"})
	})
	mux.HandleFunc("/v1/pair/X/ready", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	mux.HandleFunc("/v1/pair/X/status", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"state": "pending"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := New(srv.URL).Pair(ctx, nil); err == nil {
		t.Fatal("expected timeout error")
	}
}
