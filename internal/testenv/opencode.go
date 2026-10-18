package testenv

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// OpencodeServePeer replays the probed v1 protocol in a disposable subprocess.
// It never invokes a vendor CLI or reads any user configuration.
func OpencodeServePeer(t *testing.T) {
	if os.Getenv("COCKPIT_OC_PEER") != "1" {
		return
	}
	password := os.Getenv("OPENCODE_SERVER_PASSWORD")
	if len(password) < 32 || os.Getenv("OPENCODE_SERVER_USERNAME") != "opencode" {
		os.Exit(41)
	}
	data := filepath.Join(os.Getenv("XDG_DATA_HOME"), "opencode")
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(data, "opencode.db")
	var mu sync.Mutex
	count := 0
	if b, err := os.ReadFile(db); err == nil {
		json.Unmarshal(b, &count)
	}
	var stream chan map[string]any
	scenario := os.Getenv("COCKPIT_OC_SCENARIO")
	aborted := false
	emit := func(typ string, props map[string]any) {
		if typ != "session.error" || scenario != "unknown-no-id" {
			props["sessionID"] = "sid"
		}
		stream <- map[string]any{"type": typ, "properties": props}
	}
	apiError := func() any {
		if scenario == "quota" {
			return map[string]any{"name": "APIError", "data": map[string]any{"message": "Insufficient balance", "statusCode": 429, "isRetryable": true}}
		}
		if scenario == "transient" && count == 1 {
			return map[string]any{"name": "APIError", "data": map[string]any{"message": "Service unavailable", "statusCode": 503, "isRetryable": false}}
		}
		return nil
	}
	info := func() map[string]any {
		return map[string]any{"id": fmt.Sprintf("msg-%d", count), "sessionID": "sid", "role": "assistant", "providerID": "opencode", "modelID": "test-free", "finish": "stop", "error": apiError(), "tokens": map[string]any{"input": 1, "output": 1}, "time": map[string]int{"completed": 1}}
	}
	finish := func() {
		mu.Lock()
		defer mu.Unlock()
		if scenario == "unknown" || scenario == "unknown-no-id" {
			emit("session.error", map[string]any{"error": map[string]any{"name": "UnknownError", "data": map[string]string{"message": "Model not found: nope/x"}}})
		} else {
			emit("message.updated", map[string]any{"info": info()})

			if scenario == "opaque-metadata" {
				emit("message.part.updated", map[string]any{"part": map[string]any{"id": "opaque-1", "sessionID": "sid", "type": "tool", "tool": "bash", "state": map[string]any{"status": "completed", "output": "done", "metadata": map[string]string{"truncated": "vendor-specific"}}}})
			}
			if scenario == "truncated" {
				// Repeated completed frames must not duplicate the warning.
				for i := 0; i < 2; i++ {
					emit("message.part.updated", map[string]any{"part": map[string]any{"id": "clipped-1", "sessionID": "sid", "type": "tool", "tool": "bash", "state": map[string]any{"status": "completed", "output": "partial output", "metadata": map[string]bool{"truncated": true}}}})
				}
			}
			emit("message.part.updated", map[string]any{"part": map[string]any{"id": "tool-1", "sessionID": "sid", "type": "tool", "tool": "edit", "state": map[string]string{"status": "completed"}}})
			emit("message.part.updated", map[string]any{"part": map[string]any{"id": "text-1", "sessionID": "sid", "messageID": fmt.Sprintf("msg-%d", count), "type": "text", "text": "DONE"}})
		}
		emit("session.idle", map[string]any{})
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pw, ok := r.BasicAuth()
		if !ok || user != "opencode" || pw != password {
			http.Error(w, "Unauthorized", 401)
			return
		}
		if r.URL.Query().Get("directory") == "" {
			http.Error(w, "directory is required", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /event":
			w.Header().Set("Content-Type", "text/event-stream")
			ch := make(chan map[string]any, 256)
			mu.Lock()
			stream = ch
			mu.Unlock()
			fmt.Fprint(w, "data: {\"type\":\"server.connected\",\"properties\":{}}\n\n")
			w.(http.Flusher).Flush()
			for {
				select {
				case ev := <-ch:
					b, _ := json.Marshal(ev)
					fmt.Fprintf(w, "data: %s\n\n", b)
					w.(http.Flusher).Flush()
				case <-r.Context().Done():
					return
				}
			}
		case "GET /config/providers":
			fmt.Fprint(w, `{"providers":[{"id":"opencode","models":{"test-free":{"id":"test-free","cost":{"input":0}},"paid":{"id":"paid","cost":{"input":1}}}},{"id":"minimax-coding-plan","models":{"MiniMax-M3":{"id":"MiniMax-M3","cost":{"input":0}}}}],"default":{"opencode":"paid"}}`)
		case "POST /session", "PATCH /session/sid":
			body, _ := io.ReadAll(r.Body)
			os.WriteFile(filepath.Join(data, "permissions.json"), body, 0600)
			if _, err := os.Stat(db); os.IsNotExist(err) {
				os.WriteFile(db, []byte("0"), 0600)
			}
			fmt.Fprint(w, `{"id":"sid"}`)
		case "GET /session/sid":
			mu.Lock()
			defer mu.Unlock()
			if _, err := os.Stat(db); err != nil {
				http.Error(w, "missing session", 404)
				return
			}
			// Session rules as last sent (the real server appends PATCHed rules).
			var sent struct {
				Permission json.RawMessage `json:"permission"`
			}
			if b, err := os.ReadFile(filepath.Join(data, "permissions.json")); err == nil {
				_ = json.Unmarshal(b, &sent)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "sid", "permission": sent.Permission, "tokens": map[string]any{"input": count * 20, "output": count * 3, "reasoning": count * 7, "cache": map[string]int{"read": count * 100, "write": count * 2}}, "cost": float64(count) * .25})
		case "GET /session/sid/message":
			mu.Lock()
			defer mu.Unlock()
			msgs := []any{}
			for i := 1; i <= count; i++ {
				item := info()
				item["id"] = fmt.Sprintf("msg-%d", i)
				msgs = append(msgs, map[string]any{"info": item, "parts": []any{map[string]string{"type": "text", "text": "DONE"}}})
			}
			if scenario == "unknown" || scenario == "unknown-no-id" {
				msgs = []any{}
			}
			json.NewEncoder(w).Encode(msgs)
		case "POST /session/sid/prompt_async":
			var req struct {
				Agent string `json:"agent"`
				Model struct{ ProviderID, ModelID string }
				Parts []struct{ Text string }
			}
			if json.NewDecoder(r.Body).Decode(&req) != nil {
				http.Error(w, "invalid prompt", 400)
				return
			}
			if req.Agent != "plan" && req.Agent != "build" || req.Model.ModelID != "test-free" {
				http.Error(w, "wrong agent/model", 400)
				return
			}
			mu.Lock()
			count++
			b, _ := json.Marshal(count)
			os.WriteFile(db, b, 0600)
			mu.Unlock()
			w.WriteHeader(204)
			mu.Lock()
			emit("message.updated", map[string]any{"info": map[string]string{"id": "user-1", "sessionID": "sid", "role": "user"}})
			emit("message.part.updated", map[string]any{"part": map[string]string{"id": "user-text", "messageID": "user-1", "sessionID": "sid", "type": "text", "text": "USER PROMPT"}})
			mu.Unlock()
			if scenario == "cancel" {
				cmd := exec.Command("sh", "-c", "sleep 120")
				if err := cmd.Start(); err != nil {
					t.Error(err)
				} else {
					os.WriteFile(filepath.Join(data, "child"), []byte(fmt.Sprint(cmd.Process.Pid)), 0600)
					go cmd.Wait()
				}
				return
			}
			if scenario == "permission" || scenario == "readonly" {
				go func() {
					mu.Lock()
					defer mu.Unlock()
					emit("permission.asked", map[string]any{"id": "per-1", "permission": "edit", "patterns": []string{"out.txt"}})
				}()
			} else {
				if scenario == "ignored-delete" || scenario == "untracked-delete" {
					os.Remove("victim")
				}
				if scenario == "ignored-modify" {
					os.WriteFile("victim", []byte("changed"), 0600)
				}
				if scenario == "violation" {
					os.WriteFile("out.txt", []byte("violation"), 0600)
				}
				go finish()
			}
		case "POST /permission/per-1/reply":
			var req struct{ Reply string }
			json.NewDecoder(r.Body).Decode(&req)
			if scenario == "readonly" && req.Reply != "reject" || scenario == "permission" && req.Reply != "once" {
				http.Error(w, "wrong permission reply", 400)
				return
			}
			os.WriteFile(filepath.Join(data, "reply"), []byte(req.Reply), 0600)
			if req.Reply == "once" {
				os.WriteFile("out.txt", []byte("hi"), 0600)
			}
			fmt.Fprint(w, "true")
			go finish()
		case "GET /session/sid/children":
			fmt.Fprint(w, `[]`)
		case "GET /permission":
			fmt.Fprint(w, `[]`)
		case "POST /session/sid/abort":
			mu.Lock()
			aborted = true
			mu.Unlock()
			os.WriteFile(filepath.Join(data, "aborted"), []byte("yes"), 0600)
			fmt.Fprint(w, "true")
			// Deliberately leave the tool alive: the adapter must clean up the tree.
			go func() {
				mu.Lock()
				defer mu.Unlock()
				if stream != nil && aborted {
					emit("session.idle", map[string]any{})
				}
			}()
		default:
			http.Error(w, "unexpected API: "+r.Method+" "+r.URL.Path, 404)
		}
	})
	server := httptest.NewServer(handler)
	fmt.Println("opencode server listening on " + server.URL)
	select {}
}

// InstallOpencodeServe replaces PATH with a wrapper for this package's peer test.
func InstallOpencodeServe(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 1.19.0; exit 0; fi\n[ \"$*\" = 'serve --hostname 127.0.0.1 --port 0' ] || exit 42\nexec '" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "' -test.run='^TestOpencodeServePeer$'\n"
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("COCKPIT_OC_PEER", "1")
	t.Setenv("HOME", t.TempDir())
	data := t.TempDir()
	os.MkdirAll(filepath.Join(data, "opencode"), 0700)
	os.WriteFile(filepath.Join(data, "opencode/auth.json"), []byte("{}"), 0600)
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return filepath.Join(data, "opencode")
}
