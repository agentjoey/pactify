package cockpit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// kimiHomeFixture lays out a kimi-code 2.x home: session_index.jsonl, one session
// per entry under sessions/<workspace>/<session>/state.json, and workspaces.json.
func kimiHomeFixture(t *testing.T, sessions map[string]struct {
	ws, cwd  string
	archived bool
}, workspaces map[string]struct {
	root    string
	created time.Time
}) string {
	t.Helper()
	home := t.TempDir()
	var index []byte
	for sid, s := range sessions {
		dir := filepath.Join(home, "sessions", s.ws, sid)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		st, _ := json.Marshal(map[string]any{"id": sid, "version": 2, "cwd": s.cwd, "archived": s.archived, "custom": map[string]any{"keep": "me"}})
		if err := os.WriteFile(filepath.Join(dir, "state.json"), st, 0o600); err != nil {
			t.Fatal(err)
		}
		line, _ := json.Marshal(map[string]string{"sessionId": sid, "sessionDir": dir, "workDir": s.cwd})
		index = append(append(index, line...), '\n')
	}
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), index, 0o600); err != nil {
		t.Fatal(err)
	}
	ws := map[string]any{}
	for key, w := range workspaces {
		ws[key] = map[string]any{"root": w.root, "name": filepath.Base(w.root), "created_at": w.created.UTC().Format(time.RFC3339Nano), "last_opened_at": w.created.UTC().Format(time.RFC3339Nano)}
	}
	b, _ := json.Marshal(map[string]any{"version": 1, "workspaces": ws})
	if err := os.WriteFile(filepath.Join(home, "workspaces.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Archiving uses kimi's own flag (what `kimi session list` filters on, and what kimi
// clears itself when the session is loaded again) and keeps every other field.
func TestArchiveKimiSessionSetsFlagAndPreservesFields(t *testing.T) {
	home := kimiHomeFixture(t, map[string]struct {
		ws, cwd  string
		archived bool
	}{"session_a": {"wd_repo_1", "/repo", false}}, nil)
	dir, err := ArchiveKimiSession(home, "session_a")
	if err != nil {
		t.Fatal(err)
	}
	st := readJSONMap(t, filepath.Join(dir, "state.json"))
	if st["archived"] != true || st["cwd"] != "/repo" || st["custom"].(map[string]any)["keep"] != "me" {
		t.Fatalf("state = %v", st)
	}
	if _, err := ArchiveKimiSession(home, "session_missing"); err == nil {
		t.Fatal("unknown session must error")
	}
}

// A workspace entry that the job itself created goes away once none of its sessions
// is visible; a workspace the user already had is never touched.
func TestPruneKimiWorkspaceOnlyRemovesWhatTheJobCreated(t *testing.T) {
	jobStart := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	repo := t.TempDir()
	userRepo := t.TempDir()
	busy := t.TempDir()
	home := kimiHomeFixture(t,
		map[string]struct {
			ws, cwd  string
			archived bool
		}{
			"session_job":   {"wd_new", repo, true},
			"session_user":  {"wd_user", userRepo, true},
			"session_busy1": {"wd_busy", busy, true},
			"session_busy2": {"wd_busy", busy, false}, // still visible: keep the workspace
		},
		map[string]struct {
			root    string
			created time.Time
		}{
			"wd_new":  {repo, jobStart.Add(2 * time.Second)},
			"wd_user": {userRepo, jobStart.Add(-48 * time.Hour)},
			"wd_busy": {busy, jobStart.Add(time.Second)},
		})
	for _, tc := range []struct {
		cwd  string
		want bool
	}{{repo, true}, {userRepo, false}, {busy, false}, {t.TempDir(), false}} {
		removed, err := PruneKimiWorkspace(home, tc.cwd, jobStart)
		if err != nil || removed != tc.want {
			t.Fatalf("%s: removed=%v err=%v, want %v", tc.cwd, removed, err, tc.want)
		}
	}
	ws := readJSONMap(t, filepath.Join(home, "workspaces.json"))["workspaces"].(map[string]any)
	if _, ok := ws["wd_new"]; ok {
		t.Fatal("job-created workspace still listed")
	}
	for _, k := range []string{"wd_user", "wd_busy"} {
		if _, ok := ws[k]; !ok {
			t.Fatalf("%s must be kept", k)
		}
	}
}

// macOS temp dirs appear as /var/... and /private/var/...; both spell the same workspace.
func TestPruneKimiWorkspaceMatchesThroughSymlinks(t *testing.T) {
	jobStart := time.Now().Add(-time.Minute)
	repo := t.TempDir()
	real, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	home := kimiHomeFixture(t, map[string]struct {
		ws, cwd  string
		archived bool
	}{"session_job": {"wd_new", real, true}}, map[string]struct {
		root    string
		created time.Time
	}{"wd_new": {real, time.Now()}})
	if removed, err := PruneKimiWorkspace(home, repo, jobStart); err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
}
