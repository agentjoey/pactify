package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type fsBrowseResp struct {
	Path    string         `json:"path"`
	Parent  string         `json:"parent"`
	Entries []fsBrowseItem `json:"entries"`
}

type fsBrowseItem struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsGit   bool   `json:"isGit"`
	HasPact bool   `json:"hasPact"`
}

func TestFsBrowseNoSeat(t *testing.T) {
	srv := New(nil)
	ts := newTestServer(t, srv)

	resp, _ := http.Get(ts.URL + "/api/fs/browse")
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestFsBrowseValidDir(t *testing.T) {
	srv := New(nil)
	srv.SetSeat("test")
	ts := newTestServer(t, srv)

	dir := t.TempDir()
	// dir must be inside an allowed root (M2 confinement); make its parent a root
	// so both dir and its parent (the "up" target) are browsable.
	t.Setenv("PACTIFY_FS_ROOT", filepath.Dir(dir))
	os.Mkdir(filepath.Join(dir, "subdir"), 0o755)
	gitDir := filepath.Join(dir, "myrepo")
	os.Mkdir(gitDir, 0o755)
	os.Mkdir(filepath.Join(gitDir, ".git"), 0o755)
	pactDir := filepath.Join(dir, "pactproj")
	os.Mkdir(pactDir, 0o755)
	os.Mkdir(filepath.Join(pactDir, ".pact"), 0o755)
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(dir, ".hidden"), 0o755)
	os.Mkdir(filepath.Join(dir, ".git"), 0o755)
	os.Mkdir(filepath.Join(dir, "node_modules"), 0o755)

	resp, _ := http.Get(ts.URL + "/api/fs/browse?path=" + dir)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	var out fsBrowseResp
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()

	if out.Path != dir {
		t.Errorf("path=%q want %q", out.Path, dir)
	}
	if out.Parent != filepath.Dir(dir) {
		t.Errorf("parent=%q want %q", out.Parent, filepath.Dir(dir))
	}
	if len(out.Entries) != 3 {
		t.Fatalf("want 3 entries got %d: %+v", len(out.Entries), out.Entries)
	}
	byName := map[string]fsBrowseItem{}
	for _, e := range out.Entries {
		byName[e.Name] = e
	}
	if e, ok := byName["myrepo"]; !ok || !e.IsGit || e.HasPact {
		t.Errorf("myrepo: want isGit=true hasPact=false, got %+v", byName["myrepo"])
	}
	if e, ok := byName["pactproj"]; !ok || e.IsGit || !e.HasPact {
		t.Errorf("pactproj: want isGit=false hasPact=true, got %+v", byName["pactproj"])
	}
	if e, ok := byName["subdir"]; !ok || e.IsGit || e.HasPact {
		t.Errorf("subdir: want isGit=false hasPact=false, got %+v", byName["subdir"])
	}
}

func TestFsBrowseEmptyPathHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Mkdir(filepath.Join(home, "projects"), 0o755)

	srv := New(nil)
	srv.SetSeat("test")
	ts := newTestServer(t, srv)

	resp, _ := http.Get(ts.URL + "/api/fs/browse")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d", resp.StatusCode)
	}
	var out fsBrowseResp
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Path != home {
		t.Errorf("empty path should resolve to home: got %q want %q", out.Path, home)
	}
}

func TestFsBrowseBadPath(t *testing.T) {
	srv := New(nil)
	srv.SetSeat("test")
	ts := newTestServer(t, srv)

	// A nonexistent path INSIDE an allowed root still 400s (not a real dir).
	root := t.TempDir()
	t.Setenv("PACTIFY_FS_ROOT", root)
	resp, _ := http.Get(ts.URL + "/api/fs/browse?path=" + filepath.Join(root, "nope", "nada"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// M2 confinement: an authed seat must NOT be able to enumerate outside the
// allowed roots, walk a traversal out of them, or descend into a hidden dir.
func TestFsBrowseConfinement(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".secrets"), 0o755)
	srv := New(nil)
	srv.SetSeat("test")
	t.Setenv("PACTIFY_FS_ROOT", root)
	ts := newTestServer(t, srv)

	cases := []struct {
		name, path string
	}{
		{"outside root (/etc)", "/etc"},
		{"outside root (system)", "/"},
		{"traversal out of root", filepath.Join(root, "..", "..", "..")},
		{"hidden dir under root (~/.ssh class)", filepath.Join(root, ".secrets")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, _ := http.Get(ts.URL + "/api/fs/browse?path=" + tc.path)
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("path %q: want 403 (confined), got %d", tc.path, resp.StatusCode)
			}
			resp.Body.Close()
		})
	}
}

func TestRegistryAddWithGroup(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := newAuthorRepo(t)
	srv := New(nil)
	srv.SetSeat("test")
	ts := newTestServer(t, srv)

	resp := postJSON(t, ts.URL+"/api/registry", map[string]any{
		"path": dir, "name": "myproj", "group": "mygroup",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()

	resp, _ = http.Get(ts.URL + "/api/registry")
	var items []struct {
		Name  string `json:"name"`
		Group string `json:"group"`
	}
	json.NewDecoder(resp.Body).Decode(&items)
	resp.Body.Close()

	if len(items) != 1 || items[0].Group != "mygroup" {
		t.Fatalf("want group=mygroup, got items=%+v", items)
	}
}

func TestSetupApplyAutoRegisters(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := initBareGitDir(t)
	srv := New(nil)
	srv.SetSeat("test")
	ts := newTestServer(t, srv)

	resp := postJSON(t, ts.URL+"/api/setup/apply", map[string]any{
		"path": dir, "project": "wizproj", "group": "wizgroup",
		"seats": []map[string]any{
			{"id": "claude", "roles": []string{"orchestrator"}, "entry": "CLAUDE.md", "kind": "claude-code"},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()

	resp, _ = http.Get(ts.URL + "/api/registry")
	var items []struct {
		Name  string `json:"name"`
		Group string `json:"group"`
	}
	json.NewDecoder(resp.Body).Decode(&items)
	resp.Body.Close()

	if len(items) != 1 {
		t.Fatalf("want 1 registered project, got %d", len(items))
	}
	if items[0].Group != "wizgroup" {
		t.Fatalf("want group=wizgroup, got %q", items[0].Group)
	}
}
