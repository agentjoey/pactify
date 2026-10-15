package cockpit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// The file-level fallback writes what kimi's own archive writes (archived +
// archivedAt) and keeps every other field.
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
	if st["archived"] != true || st["archivedAt"] == nil || st["cwd"] != "/repo" || st["custom"].(map[string]any)["keep"] != "me" {
		t.Fatalf("state = %v", st)
	}
	if _, err := ArchiveKimiSession(home, "session_missing"); err == nil {
		t.Fatal("unknown session must error")
	}
}

func TestParseKimiWebToken(t *testing.T) {
	line := "  \x1b[1mToken:\x1b[0m    AbCdEf0123456789_-AbCdEf0123456789"
	if got := parseKimiWebToken(line); got != "AbCdEf0123456789_-AbCdEf0123456789" {
		t.Fatalf("token = %q", got)
	}
	if parseKimiWebToken("  Local:    http://127.0.0.1:58991/#token=x") != "" {
		t.Fatal("only the Token: line carries the token")
	}
}

// The archive call is the one kimi's own web UI makes: POST /api/v2/sessions:archive
// with the bearer token, and every requested id must come back archived.
func TestArchiveKimiSessionsHTTP(t *testing.T) {
	var gotAuth string
	var gotIDs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/sessions:archive" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotIDs = body.IDs
		ok := 0
		for _, id := range body.IDs {
			if id != "session_bad" {
				ok++
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "success", "data": map[string]any{"succeeded": ok, "failed": len(body.IDs) - ok}})
	}))
	defer srv.Close()
	if err := archiveKimiSessionsHTTP(context.Background(), srv.URL, "tok", []string{"session_a"}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" || len(gotIDs) != 1 || gotIDs[0] != "session_a" {
		t.Fatalf("auth=%q ids=%v", gotAuth, gotIDs)
	}
	if err := archiveKimiSessionsHTTP(context.Background(), srv.URL, "tok", []string{"session_a", "session_bad"}); err == nil {
		t.Fatal("a partially failed archive must be an error")
	}
}

func TestArchiveKimiServerUsesExplicitHome(t *testing.T) {
	bin, inherited, actual, record := t.TempDir(), t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "home")
	script := "#!/bin/sh\nprintf '%s' \"$KIMI_CODE_HOME\" > \"$PACTIFY_TEST_KIMI_HOME_RECORD\"\n"
	if err := os.WriteFile(filepath.Join(bin, "kimi"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KIMI_CODE_HOME", inherited)
	t.Setenv("PACTIFY_TEST_KIMI_HOME_RECORD", record)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The fake server records its environment and exits without an API token.
	// The timeout is expected; only the process boundary is under test here.
	if err := ArchiveKimiSessionsViaServer(ctx, []string{"probe"}, actual); err == nil {
		t.Fatal("fake web server unexpectedly archived a session")
	}
	data, err := os.ReadFile(record)
	if err != nil || string(data) != actual {
		t.Fatalf("web home=%q err=%v", data, err)
	}
}
