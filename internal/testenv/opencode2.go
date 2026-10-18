package testenv

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// InstallOpencode2 installs a synthetic protocol peer, never a vendor binary.
func InstallOpencode2(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'opencode v2.0.32'; exit; fi\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run=^TestOpencode2Peer$\n"
	if err = os.WriteFile(filepath.Join(dir, "opencode"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("COCKPIT_OC2_PEER", "1")
	t.Setenv("COCKPIT_OC2_REQUESTS", filepath.Join(dir, "requests"))
	for _, key := range []string{"HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, t.TempDir())
	}
}
func Opencode2Peer(t *testing.T) {
	if os.Getenv("COCKPIT_OC2_PEER") != "1" {
		return
	}
	pw := os.Getenv("OPENCODE_PASSWORD")
	if len(pw) != 64 {
		os.Exit(42)
	}
	scenario := os.Getenv("COCKPIT_OC2_SCENARIO")
	var mu sync.Mutex
	var stream chan map[string]any
	prompt := ""
	turns := 0
	active := false
	connections := 0
	interrupts := 0
	var quietUntil time.Time // heartbeat-quiet: the tool is still running until then
	data := filepath.Join(os.Getenv("XDG_DATA_HOME"), "opencode")
	os.MkdirAll(data, 0700)
	os.WriteFile(filepath.Join(data, "opencode.db"), []byte("synthetic"), 0600)
	emit := func(kind, sid string, p map[string]any) {
		p["sessionID"] = sid
		stream <- map[string]any{"type": kind, "data": p}
	}
	failure := func() map[string]any {
		typ := "provider.quota"
		if strings.HasPrefix(scenario, "provider.") {
			typ = scenario
		}
		return map[string]any{"type": typ, "message": "Synthetic provider failure"}
	}
	finish := func() {
		active = false
		if scenario == "failure" || strings.HasPrefix(scenario, "provider.") {
			emit("session.execution.failed", "ses_root", map[string]any{"error": failure()})
			return
		}
		emit("session.step.started", "ses_root", map[string]any{"assistantMessageID": "msg_assistant", "model": map[string]string{"providerID": "opencode", "id": "test-free", "variant": "low"}})
		emit("session.text.delta", "ses_other", map[string]any{"delta": "FOREIGN"})
		emit("session.text.delta", "ses_root", map[string]any{"assistantMessageID": "msg_assistant", "ordinal": 0, "delta": "partial"})
		emit("session.text.ended", "ses_root", map[string]any{"assistantMessageID": "msg_assistant", "ordinal": 0, "text": "STREAM_FINAL"})
		emit("session.execution.succeeded", "ses_root", map[string]any{})
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "opencode" || password != pw {
			http.Error(w, "unauthorized", 401)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		defer mu.Unlock()
		f, _ := os.OpenFile(os.Getenv("COCKPIT_OC2_REQUESTS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		json.NewEncoder(f).Encode(map[string]any{"method": r.Method, "path": r.URL.Path, "query": r.URL.RawQuery, "body": req})
		f.Close()
		w.Header().Set("Content-Type", "application/json")
		write := func(v any) { json.NewEncoder(w).Encode(map[string]any{"data": v}) }
		switch r.Method + " " + r.URL.Path {
		case "GET /api/model":
			write([]any{map[string]any{"id": "test-free", "providerID": "opencode", "enabled": true, "variants": []any{map[string]string{"id": "low"}}}, map[string]any{"id": "disabled", "providerID": "opencode", "enabled": false}})
		case "GET /api/model/default":
			if scenario == "default-missing" {
				fmt.Fprint(w, `{}`)
				return
			}
			if scenario == "default-null" {
				write(nil)
				return
			}
			write(map[string]any{"id": "test-free", "providerID": "opencode", "enabled": true})
		case "GET /api/integration":
			// 2.0.32 lists every connectable provider; /api/provider lists only connected ones.
			write([]any{map[string]any{"id": "opencode", "connections": []any{}}, map[string]any{"id": "minimax-coding-plan", "connections": []any{}}})
		case "POST /api/session":
			write(map[string]string{"id": "ses_root"})
		case "GET /api/session/ses_missing":
			w.WriteHeader(404)
			fmt.Fprint(w, `{"_tag":"SessionNotFoundError","message":"Session not found"}`)
		case "GET /api/session/ses_root", "GET /api/session/ses_child", "GET /api/session/ses_grandchild":
			multiplier := turns
			if r.URL.Path != "/api/session/ses_root" {
				multiplier = 1
			}
			write(map[string]any{"id": strings.TrimPrefix(r.URL.Path, "/api/session/"), "tokens": map[string]any{"input": 10 * multiplier, "output": 2 * multiplier, "reasoning": 3 * multiplier, "cache": map[string]int{"read": 4 * multiplier, "write": 1 * multiplier}}, "cost": float64(multiplier) * .25})
		case "GET /api/session":
			parent := r.URL.Query().Get("parentID")
			if (turns > 0 || scenario == "resume-children") && parent == "ses_root" {
				if r.URL.Query().Get("cursor") == "" {
					fmt.Fprint(w, `{"data":[],"cursor":{"next":"children-page2"}}`)
				} else {
					fmt.Fprint(w, `{"data":[{"id":"ses_child","parentID":"ses_root"}],"cursor":{}}`)
				}
			} else if (turns > 0 || scenario == "resume-children") && parent == "ses_child" {
				fmt.Fprint(w, `{"data":[{"id":"ses_grandchild","parentID":"ses_child"}],"cursor":{}}`)
			} else {
				fmt.Fprint(w, `{"data":[],"cursor":{}}`)
			}
		case "GET /api/event":
			connections++
			if scenario == "reconnect-exhausted" && connections > 1 {
				http.Error(w, "unavailable", 503)
				return
			}
			ch := make(chan map[string]any, 100)
			stream = ch
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"type\":\"server.connected\",\"data\":{}}\n\n")
			w.(http.Flusher).Flush()
			mu.Unlock()
			defer mu.Lock()
			quiet := scenario == "heartbeat-quiet"
			for {
				select {
				case e := <-ch:
					if quiet && e["type"] == "session.tool.called" {
						// A long silent tool: only SSE heartbeat comments reach the client.
						quiet = false
						mu.Lock()
						quietUntil = time.Now().Add(400 * time.Millisecond)
						mu.Unlock()
						for deadline := time.Now().Add(400 * time.Millisecond); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
							fmt.Fprint(w, ": heartbeat\n\n")
							w.(http.Flusher).Flush()
						}
					}
					b, _ := json.Marshal(e)
					fmt.Fprintf(w, "data: %s\n\n", b)
					w.(http.Flusher).Flush()
					if strings.HasPrefix(scenario, "reconnect") && connections == 1 && (e["type"] == "session.inbox.delivered" || scenario == "reconnect-missed-delivery" && e["type"] == "session.step.started") {
						mu.Lock()
						if scenario == "reconnect" || scenario == "reconnect-missing-boundary" || scenario == "reconnect-no-outcome" || scenario == "reconnect-interrupted" {
							active = false
						}
						mu.Unlock()
						return
					}
				case <-r.Context().Done():
					return
				}
			}
		case "POST /api/session/ses_root/prompt":
			if stream == nil {
				http.Error(w, "not subscribed", 500)
				return
			}
			prompt, _ = req["id"].(string)
			if !strings.HasPrefix(prompt, "msg_") {
				http.Error(w, "invalid id", 400)
				return
			}
			turns++
			active = true
			// An old success before delivery must not end the new turn.
			emit("session.execution.succeeded", "ses_root", map[string]any{})
			if scenario == "early-failure" {
				active = false
				emit("session.execution.failed", "ses_root", map[string]any{"error": failure()})
				write(map[string]string{"id": prompt})
				return
			}
			if scenario != "reconnect-missed-delivery" && scenario != "cancel-before-delivery" {
				emit("session.inbox.delivered", "ses_root", map[string]any{"inboxID": prompt})
			} else if scenario == "reconnect-missed-delivery" {
				emit("session.step.started", "ses_root", map[string]any{})
			}
			emit("session.created", "ses_child", map[string]any{"parentID": "ses_root"})
			emit("session.created", "ses_grandchild", map[string]any{"parentID": "ses_child"})
			switch scenario {
			case "permission", "readonly":
				emit("permission.asked", "ses_grandchild", map[string]any{"id": "per_1", "action": "shell", "resources": []string{"echo x > probe.txt"}})
			case "cancel", "cancel-real-process", "cancel-before-delivery", "user", "shutdown", "inactivity":
				if scenario == "cancel" || scenario == "cancel-real-process" {
					if scenario == "cancel-real-process" {
						cmd := exec.Command("sleep", "60")
						if err := cmd.Start(); err != nil {
							http.Error(w, "sleep start failed", 500)
							return
						}
						go cmd.Wait()
						emit("shell.created", "", map[string]any{"info": map[string]any{"id": "sh_sleep", "command": "sleep 60", "pid": cmd.Process.Pid, "metadata": map[string]string{"sessionID": "ses_root"}}})
					}
					emit("session.tool.called", "ses_root", map[string]any{"assistantMessageID": "msg_sleep", "id": "call_sleep", "name": "shell", "input": map[string]string{"command": "sleep 60"}})
				}
				if scenario != "cancel" && scenario != "cancel-real-process" && scenario != "cancel-before-delivery" {
					active = false
					emit("session.execution.interrupted", "ses_root", map[string]any{"reason": scenario})
				}
			case "reconnect", "reconnect-permission", "reconnect-wait", "reconnect-missing-boundary", "reconnect-no-outcome", "reconnect-exhausted", "reconnect-missed-delivery", "reconnect-interrupted", "stream-stall":
			case "heartbeat-quiet":
				fallthrough
			default:
				emit("session.reasoning.delta", "ses_root", map[string]any{"delta": "NOISY_REASONING"})
				emit("session.tool.progress", "ses_root", map[string]any{})
				emit("session.usage.updated", "ses_root", map[string]any{})
				emit("permission.rejected", "ses_root", map[string]any{})
				emit("model.updated", "", map[string]any{})
				emit("provider.updated", "", map[string]any{})
				emit("integration.updated", "", map[string]any{})
				emit("session.tool.input.started", "ses_root", map[string]any{"assistantMessageID": "msg_assistant", "id": "call1", "name": "shell"})
				emit("session.tool.called", "ses_root", map[string]any{"assistantMessageID": "msg_assistant", "id": "call1", "input": map[string]string{"command": "git status"}})
				emit("session.tool.success", "ses_root", map[string]any{"assistantMessageID": "msg_assistant", "id": "call1", "content": []any{map[string]string{"type": "text", "text": "output"}}, "metadata": map[string]bool{"truncated": true}})
				emit("session.tool.success", "ses_root", map[string]any{"assistantMessageID": "msg_assistant", "id": "call2", "content": []any{}, "metadata": map[string]bool{"truncated": true}})
				emit("form.created", "ses_root", map[string]any{"form": map[string]string{"id": "form1", "sessionID": "ses_root"}})
				finish()
			}
			write(map[string]string{"id": prompt})
		case "POST /api/session/ses_grandchild/permission/per_1/reply":
			if req["decision"] == "always" || req["decision"] == "reject" && req["message"] == nil {
				http.Error(w, "unsafe reply", 400)
				return
			}
			emit("session.tool.input.started", "ses_grandchild", map[string]any{"assistantMessageID": "msg_child", "id": "call1", "name": "shell"})
			emit("session.tool.called", "ses_grandchild", map[string]any{"assistantMessageID": "msg_child", "id": "call1", "input": map[string]string{"command": "echo x > probe.txt"}})
			emit("session.tool.failed", "ses_grandchild", map[string]any{"assistantMessageID": "msg_child", "id": "call1", "error": map[string]string{"type": "permission.rejected", "message": "Permission denied"}})
			finish()
			w.WriteHeader(204)
		case "DELETE /api/session/ses_root/form/form1":
			if r.URL.Query().Get("message") == "" {
				http.Error(w, "missing message", 400)
				return
			}
			w.WriteHeader(204)
		case "GET /api/session/ses_root/message":
			if scenario == "history-failure" {
				http.Error(w, "unavailable", 503)
				return
			}
			outcome := "succeeded"
			var historyErr any
			if scenario == "failure" || strings.HasPrefix(scenario, "provider.") || scenario == "early-failure" {
				outcome, historyErr = "failed", failure()
			}
			if scenario == "cancel" || scenario == "user" || scenario == "shutdown" || scenario == "inactivity" || scenario == "reconnect-interrupted" {
				outcome = "interrupted"
			}
			if r.URL.Query().Get("cursor") == "" {
				messages := []any{}
				if scenario == "background" {
					messages = append(messages, map[string]any{"id": "later_idle", "type": "idle", "outcome": "failed", "error": failure()}, map[string]any{"id": "later_answer", "type": "assistant", "time": map[string]int{"completed": 2}, "content": []any{map[string]string{"type": "text", "text": "LATER_WAKE"}}})
				}
				if scenario != "reconnect-no-outcome" && scenario != "stream-stall" && !(scenario == "reconnect-missed-delivery" && active) && !time.Now().Before(quietUntil) {
					messages = append(messages, map[string]any{"id": "msg_idle", "type": "idle", "outcome": outcome, "error": historyErr})
				}
				messages = append(messages, map[string]any{"id": "msg_assistant", "type": "assistant", "time": map[string]int{"completed": 1}, "content": []any{map[string]string{"type": "text", "text": "FINAL"}, map[string]string{"type": "text", "text": "_TEXT"}}})
				json.NewEncoder(w).Encode(map[string]any{"data": messages, "cursor": map[string]string{"next": "message-page2"}})
			} else {
				id := prompt
				if scenario == "reconnect-missing-boundary" {
					id = "other_prompt"
				}
				json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"id": id, "type": "user"}}, "cursor": map[string]any{}})
			}
		case "GET /api/session/active":
			if scenario == "reconnect-missed-delivery" && active && connections > 1 {
				finish()
				write(map[string]any{"ses_root": map[string]string{"type": "running"}})
				return
			}
			if active || time.Now().Before(quietUntil) {
				write(map[string]any{"ses_root": map[string]string{"type": "running"}})
			} else if scenario == "background" {
				write(map[string]any{"ses_child": map[string]string{"type": "running"}})
			} else {
				write(map[string]any{})
			}
		case "GET /api/session/ses_root/permission", "GET /api/session/ses_child/permission", "GET /api/session/ses_grandchild/permission":
			if scenario == "reconnect-permission" && r.URL.Path == "/api/session/ses_root/permission" && active {
				write([]any{map[string]any{"id": "per_lost", "sessionID": "ses_root", "action": "shell", "resources": []string{"git status"}, "metadata": map[string]string{"note": "retained"}}})
			} else {
				write([]any{})
			}
		case "POST /api/session/ses_root/permission":
			effect := "deny"
			if action, _ := req["action"].(string); action == os.Getenv("COCKPIT_OC2_ALLOW_ACTION") {
				effect = "allow"
			}
			write(map[string]string{"effect": effect})
		case "POST /api/session/ses_root/permission/per_lost/reply":
			finish()
			w.WriteHeader(204)
		case "POST /api/experimental/session/ses_root/wait":
			if scenario == "reconnect-missed-delivery" {
				http.Error(w, "wait unavailable", 503)
				return
			}
			active = false
			w.WriteHeader(204)
		case "POST /api/session/ses_root/interrupt":
			interrupts++
			if scenario == "cancel-before-delivery" && interrupts == 1 {
				emit("session.inbox.delivered", "ses_root", map[string]any{"inboxID": prompt})
				// 2.0.32 returns SessionInterruptResponse without the data envelope.
				json.NewEncoder(w).Encode(map[string]bool{"interrupted": false})
				return
			}
			active = false
			emit("session.execution.interrupted", "ses_root", map[string]any{"reason": "user"})
			json.NewEncoder(w).Encode(map[string]bool{"interrupted": true})
		case "POST /api/session/ses_root/model":
			if model, ok := req["model"].(map[string]any); ok && model["id"] == "pactify-nonexistent-model" {
				scenario = "provider.no-route"
			}
			w.WriteHeader(204)
		case "PATCH /api/session/ses_child", "PATCH /api/session/ses_grandchild", "PATCH /api/session/ses_root", "POST /api/session/ses_root/agent", "PUT /api/experimental/session/ses_root/instructions/entries/pactify-system":
			w.WriteHeader(204)
		default:
			http.Error(w, "unhandled "+r.Method+" "+r.URL.Path, 500)
		}
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(43)
	}
	fmt.Printf("{\"url\":\"http://%s\"}\n", listener.Addr())
	go func() { io.Copy(io.Discard, os.Stdin); listener.Close() }()
	_ = http.Serve(listener, handler)
	os.Exit(0)
}
