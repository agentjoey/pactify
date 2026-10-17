package sub

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

// A fake stdio app-server drives the production Codex JSON-RPC parser. It never
// invokes the installed codex executable, opens login files or accesses a model.
func TestCodexFailureProtocolPeer(t *testing.T) {
	mode := os.Getenv("AJ718_CODEX_FAILURE")
	if mode == "" {
		return
	}
	scan, enc := bufio.NewScanner(os.Stdin), json.NewEncoder(os.Stdout)
	for scan.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scan.Bytes(), &request) != nil {
			os.Exit(91)
		}
		if len(request.ID) == 0 {
			continue
		}
		result := map[string]any{}
		if request.Method == "thread/start" {
			result = map[string]any{"thread": map[string]string{"id": "aj718-thread"}, "model": "fake-model"}
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		if request.Method != "turn/start" {
			continue
		}
		if mode == "reconnect" {
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "error", "params": map[string]any{"threadId": "aj718-thread", "turnId": "turn-1", "willRetry": true, "error": map[string]string{"message": "Reconnecting... 1/5 (rate limit reached…)"}}})
		}
		message, code := "tool sandbox crashed", "other"
		if mode == "quota" {
			message, code = "original provider quota failure", "usageLimitExceeded"
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"threadId": "aj718-thread", "turn": map[string]any{"id": "turn-1", "status": "failed", "error": map[string]string{"message": message, "codexErrorInfo": code}}}})
	}
	os.Exit(0)
}

func TestWorkerCodexFailureNotificationsThroughAdapter(t *testing.T) {
	for _, mode := range []string{"reconnect", "quota"} {
		t.Run(mode, func(t *testing.T) {
			store, _, _ := workerFixture(t, "write")
			if err := store.WithState(context.Background(), "j1", func(st *State) error { st.Request.Harness = "codex"; return store.Save(*st) }); err != nil {
				t.Fatal(err)
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			script := fmt.Sprintf("#!/bin/sh\nexec '%s' -test.run=^TestCodexFailureProtocolPeer$ -- \"$@\"\n", strings.ReplaceAll(exe, "'", "'\\''"))
			if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("AJ718_CODEX_FAILURE", mode)
			h := Harness{Name: "codex", Kind: "codex-cli", Backend: cockpit.NewCodexBackend}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			r, err := RunJob(ctx, store, "j1", h, WorkerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			st, err := store.Load("j1")
			if err != nil {
				t.Fatal(err)
			}
			class, wantRoutes := "other", 0
			if mode == "quota" {
				class, wantRoutes = "quota", 1
			}
			if r.Status != StatusFailed || r.FailureClass != class || st.Error != "Turn failed" || len(r.Retries) != 0 {
				t.Errorf("class=%s error=%q status=%s retries=%d", r.FailureClass, st.Error, r.Status, len(r.Retries))
			}
			routes, err := store.ExhaustedRoutes(time.Now())
			if err != nil || len(routes) != wantRoutes {
				t.Errorf("quota routes=%v err=%v", routes, err)
			}
		})
	}
}
