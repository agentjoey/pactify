package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This peer accepts only the non-billable handshake/catalog protocol. Any
// thread, turn or unexpected method terminates the fake server immediately.
func TestCodexCatalogProcessHelper(t *testing.T) {
	if os.Getenv("CODEX_CATALOG_HELPER") != "1" {
		return
	}
	args, _ := json.Marshal(os.Args)
	_ = os.WriteFile(os.Getenv("CODEX_CATALOG_ARGS"), args, 0600)
	_ = os.WriteFile(os.Getenv("CODEX_CATALOG_PID"), []byte(strconv.Itoa(os.Getpid())), 0600)
	log, err := os.OpenFile(os.Getenv("CODEX_CATALOG_CALLS"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(90)
	}
	defer log.Close()
	decoder := json.NewDecoder(os.Stdin)
	initialized := false
	for {
		var m wireMessage
		if decoder.Decode(&m) != nil {
			os.Exit(0)
		}
		fmt.Fprintln(log, m.Method)
		result := any(map[string]any{})
		switch m.Method {
		case "initialize":
		case "initialized":
			initialized = true
			continue
		case "model/list":
			if !initialized {
				os.Exit(91)
			}
			switch os.Getenv("CODEX_CATALOG_MODE") {
			case "hang":
				time.Sleep(time.Minute)
				continue
			case "error":
				mustWriteJSON(os.Stdout, map[string]any{"id": m.ID, "error": map[string]any{"code": -1, "message": "catalog offline"}})
				continue
			case "malformed":
				result = map[string]any{"data": "invalid"}
			case "repeat":
				result = map[string]any{"data": []any{map[string]any{"model": "one"}}, "nextCursor": "again"}
			case "empty":
				result = map[string]any{"data": []any{}}
			default:
				var p struct {
					Cursor string `json:"cursor"`
				}
				_ = json.Unmarshal(m.Params, &p)
				if p.Cursor == "next" {
					result = map[string]any{"data": []any{map[string]any{"model": "gpt-two", "supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "medium"}, map[string]any{"reasoningEffort": "high"}}}}}
				} else {
					result = map[string]any{"data": []any{map[string]any{"model": "gpt-one", "supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "low"}, map[string]any{"reasoningEffort": "medium"}}, "provider_key": "DO_NOT_RETURN"}}, "nextCursor": "next"}
				}
			}
		default:
			os.Exit(92)
		}
		mustWriteJSON(os.Stdout, map[string]any{"id": m.ID, "result": result})
	}
}

func codexCatalogFixture(t *testing.T) (StartOpts, string, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls")
	args := filepath.Join(t.TempDir(), "args")
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "' -test.run='^TestCodexCatalogProcessHelper$' -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return StartOpts{RepoDir: t.TempDir(), MinimalConfig: true, Stderr: io.Discard, Env: []string{"CODEX_CATALOG_HELPER=1", "CODEX_CATALOG_CALLS=" + calls, "CODEX_CATALOG_ARGS=" + args, "CODEX_CATALOG_PID=" + args + ".pid", "CODEX_HOME=" + t.TempDir()}}, calls, args
}
func TestCodexCatalogDiscovery(t *testing.T) {
	opts, calls, args := codexCatalogFixture(t)
	opts.Hidden = true // Close must still avoid thread/archive when no thread was created.
	got, err := CodexModelDetails(context.Background(), opts)
	if err != nil || !reflect.DeepEqual(got.Items, []string{"gpt-one", "gpt-two"}) || !reflect.DeepEqual(got.Efforts, []string{"low", "medium", "high"}) {
		t.Fatalf("%+v %v", got, err)
	}
	data, _ := os.ReadFile(calls)
	if string(data) != "initialize\ninitialized\nmodel/list\nmodel/list\n" {
		t.Fatal(string(data))
	}
	data, _ = os.ReadFile(args)
	for _, want := range []string{"app-server", "features.plugins=false", "features.apps=false", "features.skip_host_skill_discovery=true", "memories.generate_memories=false"} {
		if !strings.Contains(string(data), want) {
			t.Fatal(want, string(data))
		}
	}
	pidData, err := os.ReadFile(args + ".pid")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidData))
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(until) {
			t.Fatal("catalog app-server was not stopped")
		}
		time.Sleep(10 * time.Millisecond)
	}

	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "DO_NOT_RETURN") {
		t.Fatal(string(encoded))
	}
	items, err := CodexModels(context.Background(), opts)
	if err != nil || !reflect.DeepEqual(items, got.Items) {
		t.Fatal(items, err)
	}
}
func TestCodexCatalogFailuresAndTimeout(t *testing.T) {
	for _, mode := range []string{"error", "malformed", "repeat", "empty", "hang"} {
		t.Run(mode, func(t *testing.T) {
			opts, calls, _ := codexCatalogFixture(t)
			opts.Env = append(opts.Env, "CODEX_CATALOG_MODE="+mode)
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			started := time.Now()
			_, err := CodexModels(ctx, opts)
			if err == nil || !strings.Contains(err.Error(), "codex:") {
				t.Fatal(err)
			}
			if time.Since(started) > 3*time.Second {
				t.Fatal("discovery did not respect its deadline")
			}
			data, _ := os.ReadFile(calls)
			if strings.Contains(string(data), "thread/") || strings.Contains(string(data), "turn/") {
				t.Fatal(string(data))
			}
		})
	}
}

func TestCodexCatalogWithoutEffortMetadata(t *testing.T) {
	peer := newFakeCodexServer(t)
	peer.handle("model/list", func(wireMessage) any { return map[string]any{"data": []any{map[string]any{"model": "plain-model"}}} })
	got, err := peer.client.modelDetails(context.Background())
	if err != nil || !reflect.DeepEqual(got.Items, []string{"plain-model"}) || len(got.Efforts) != 0 {
		t.Fatal(got, err)
	}
}
