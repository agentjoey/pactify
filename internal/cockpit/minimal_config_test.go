package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the process boundary without loading an installed harness or credentials.
func TestMinimalConfigCodexPeer(t *testing.T) {
	if os.Getenv("MINIMAL_CODEX_PEER") != "1" {
		return
	}
	log, err := os.Create(os.Getenv("MINIMAL_CODEX_REQUESTS"))
	if err != nil {
		os.Exit(2)
	}
	defer log.Close()
	in := json.NewDecoder(os.Stdin)
	for {
		var m wireMessage
		if in.Decode(&m) != nil {
			return
		}
		_ = json.NewEncoder(log).Encode(m)
		if m.ID == 0 {
			continue
		}
		result := any(map[string]any{})
		switch m.Method {
		case "config/read":
			if os.Getenv("MINIMAL_CODEX_CONFIG_ERROR") == "1" {
				mustWriteJSON(os.Stdout, map[string]any{"id": m.ID, "error": map[string]any{"code": -1, "message": "config unavailable"}})
				continue
			}
			servers := map[string]any{}
			for i := 0; i < 17; i++ {
				servers[fmt.Sprintf("private.server.%d", i)] = map[string]any{"command": "private-command", "enabled": true}
			}
			result = map[string]any{"config": map[string]any{"mcp_servers": servers}}
		case "thread/start", "thread/resume":
			result = map[string]any{"thread": map[string]any{"id": "hidden-thread"}, "model": "chosen-model", "modelProvider": "chosen-provider"}
		}
		mustWriteJSON(os.Stdout, map[string]any{"id": m.ID, "result": result})
	}
}

func minimalConfigCodexProcess(t *testing.T) (string, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	args, requests := filepath.Join(dir, "args"), filepath.Join(dir, "requests")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + args + "'\nexec '" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "' -test.run='^TestMinimalConfigCodexPeer$'\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MINIMAL_CODEX_PEER", "1")
	t.Setenv("MINIMAL_CODEX_REQUESTS", requests)
	return args, requests
}

func TestMinimalConfigCodexStartAndResume(t *testing.T) {
	for _, minimal := range []bool{true, false} {
		for _, resume := range []bool{true, false} {
			t.Run(fmt.Sprintf("minimal=%v/resume=%v", minimal, resume), func(t *testing.T) {
				argsPath, requestsPath := minimalConfigCodexProcess(t)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				opts := StartOpts{RepoDir: t.TempDir(), Hidden: true, Model: "chosen-model", Access: AccessWrite, MinimalConfig: minimal}
				b := NewCodexBackend()
				var s Session
				var err error
				if resume {
					s, err = b.(OptsResumer).ResumeWith(ctx, "hidden-thread", opts)
				} else {
					s, err = b.Start(ctx, opts)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				args, _ := os.ReadFile(argsPath)
				for _, flag := range []string{"features.plugins=false", "features.apps=false", "features.skip_host_skill_discovery=true", "skills.include_instructions=false", "memories.use_memories=false", "memories.generate_memories=false"} {
					if strings.Contains(string(args), flag) != minimal {
						t.Errorf("minimal=%v: args missing/unexpected %s: %s", minimal, flag, args)
					}
				}
				if strings.Contains(string(args), "CODEX_HOME") || strings.Contains(string(args), "model=") || strings.Contains(string(args), "sandbox=") {
					t.Fatalf("user connection/settings overridden: %s", args)
				}
				data, _ := os.ReadFile(requestsPath)
				read, archived, started := false, false, false
				for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
					var m wireMessage
					if err := json.Unmarshal([]byte(line), &m); err != nil {
						t.Fatal(err)
					}
					switch m.Method {
					case "config/read":
						if started {
							t.Fatal("MCP discovery ran after thread startup")
						}
						read = true
					case "thread/start", "thread/resume":
						started = true
						var p struct {
							Config map[string]json.RawMessage `json:"config"`
						}
						_ = json.Unmarshal(m.Params, &p)
						var servers map[string]struct{ Enabled bool }
						_ = json.Unmarshal(p.Config["mcp_servers"], &servers)
						if minimal && len(servers) != 17 || !minimal && len(servers) != 0 {
							t.Fatalf("MCP overrides=%s", m.Params)
						}
						for name, server := range servers {
							if server.Enabled || !strings.HasPrefix(name, "private.server.") {
								t.Fatalf("server name was split or enabled: %s %+v", name, server)
							}
						}
						if !strings.Contains(string(p.Config["sandbox_workspace_write"]), `"network_access":true`) {
							t.Fatalf("lost write-mode networking: %s", m.Params)
						}
					case "thread/archive":
						archived = true
					}
				}
				if read != minimal || !archived || !started {
					t.Fatalf("read=%v archive=%v start=%v", read, archived, started)
				}
			})
		}
	}
}

func TestMinimalConfigCodexDiscoveryFailureDoesNotStartThread(t *testing.T) {
	_, requests := minimalConfigCodexProcess(t)
	t.Setenv("MINIMAL_CODEX_CONFIG_ERROR", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := NewCodexBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), MinimalConfig: true})
	if err == nil || !strings.Contains(err.Error(), "config unavailable") {
		t.Fatalf("error=%v", err)
	}
	data, _ := os.ReadFile(requests)
	if strings.Contains(string(data), `"thread/start"`) {
		t.Fatal("started a thread with private tools after config discovery failed")
	}
}
