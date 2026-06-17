package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupApplyNoSeat(t *testing.T) {
	srv := New(nil)
	ts := newTestServer(t, srv)

	resp := postJSON(t, ts.URL+"/api/setup/apply", map[string]any{
		"path": t.TempDir(), "project": "p", "seats": []any{},
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSetupApplyAlreadyInit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := newAuthorRepo(t)
	srv := New(nil)
	srv.SetSeat("test")
	ts := newTestServer(t, srv)

	resp := postJSON(t, ts.URL+"/api/setup/apply", map[string]any{
		"path": dir, "project": "p",
		"seats": []map[string]any{
			{"id": "alice", "roles": []string{"worker"}, "entry": "AGENTS.md"},
		},
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("want 409 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
}

func initBareGitDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0o644)
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "base"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		c.CombinedOutput()
	}
	return dir
}

func TestSetupApplySuccess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := initBareGitDir(t)
	srv := New(nil)
	srv.SetSeat("test")
	ts := newTestServer(t, srv)

	resp := postJSON(t, ts.URL+"/api/setup/apply", map[string]any{
		"path": dir, "project": "myproj",
		"seats": []map[string]any{
			{"id": "claude", "roles": []string{"orchestrator", "reviewer"}, "entry": "CLAUDE.md", "kind": "claude-code"},
			{"id": "opencode", "roles": []string{"worker"}, "entry": "AGENTS.md", "kind": "opencode"},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	var out setupApplyResp
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()

	if !out.Inited {
		t.Fatal("inited should be true")
	}
	if len(out.Wired) != 2 {
		t.Fatalf("want 2 wired, got %d: %+v", len(out.Wired), out.Wired)
	}

	if _, err := os.Stat(filepath.Join(dir, ".pact", "STATE.yml")); err != nil {
		t.Fatalf(".pact/STATE.yml should exist: %v", err)
	}

	for _, w := range out.Wired {
		if !w.Wrote {
			t.Errorf("wired %s should have wrote=true", w.Kind)
		}
		if w.DocOnly {
			t.Errorf("wired %s should not be docOnly", w.Kind)
		}
	}
}

func TestSetupApplyTOMLDocOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := initBareGitDir(t)
	srv := New(nil)
	srv.SetSeat("test")
	ts := newTestServer(t, srv)

	resp := postJSON(t, ts.URL+"/api/setup/apply", map[string]any{
		"path": dir, "project": "docproj",
		"seats": []map[string]any{
			{"id": "test", "roles": []string{"orchestrator"}, "entry": "AGENTS.md", "kind": "codex-cli"},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	var out setupApplyResp
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()

	if !out.Inited {
		t.Fatal("inited should be true")
	}
	if len(out.Wired) != 1 {
		t.Fatalf("want 1 wired, got %d", len(out.Wired))
	}
	w := out.Wired[0]
	if !w.DocOnly {
		t.Fatal("codex-cli must be docOnly")
	}
	if !strings.Contains(w.Snippet, "[mcp_servers.pact]") {
		t.Fatalf("codex snippet must carry TOML table:\n%s", w.Snippet)
	}
	if _, err := os.Stat(filepath.Join(dir, ".codex", "config.toml")); err == nil {
		t.Fatal("codex config.toml must not be written (doc-only)")
	}
}
