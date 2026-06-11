package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// getWiring fetches /api/projects/{id}/wiring.
func getWiring(t *testing.T, baseURL, id string) []struct {
	Kind    string `json:"kind"`
	Wired   bool   `json:"wired"`
	Detail  string `json:"detail"`
	Global  bool   `json:"global"`
	DocOnly bool   `json:"docOnly"`
} {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/projects/" + id + "/wiring")
	if err != nil {
		t.Fatalf("GET wiring: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wiring: want 200 got %d", resp.StatusCode)
	}
	var out []struct {
		Kind    string `json:"kind"`
		Wired   bool   `json:"wired"`
		Detail  string `json:"detail"`
		Global  bool   `json:"global"`
		DocOnly bool   `json:"docOnly"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	return out
}

// TestWiringGETUnwired: a repo with no baked entry files and no agent configs
// reports every kind unwired (content-aware: file presence alone is not wiring).
func TestWiringGETUnwired(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir() // no entry files, no configs
	ts := authorServer(t, dir, "")
	for _, row := range getWiring(t, ts.URL, "pactify") {
		if row.Wired {
			t.Fatalf("kind %q should be unwired in a bare dir: %+v", row.Kind, row)
		}
	}
}

// TestWiringGETDetectsWired: after wiring opencode, the GET reflects it.
func TestWiringGETDetectsWired(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := newAuthorRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{"mcp":{"pact":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := authorServer(t, dir, "")
	rows := getWiring(t, ts.URL, "pactify")
	var got bool
	for _, row := range rows {
		if row.Kind == "opencode" {
			got = row.Wired
			if !strings.Contains(row.Detail, "opencode.json") {
				t.Fatalf("opencode detail = %q", row.Detail)
			}
		}
	}
	if !got {
		t.Fatalf("opencode row should be wired: %+v", rows)
	}
}

// TestWirePOSTProjectKind: POST opencode writes opencode.json into the repo dir
// and the config now contains the pact server.
func TestWirePOSTProjectKind(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "")
	resp := postJSON(t, ts.URL+"/api/projects/pactify/wiring/opencode", map[string]any{"seat": "opencode", "roles": "worker"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wire opencode: want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	var out struct {
		Wrote   bool   `json:"wrote"`
		Global  bool   `json:"global"`
		Path    string `json:"path"`
		DocOnly bool   `json:"docOnly"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if !out.Wrote || out.Global || out.DocOnly || out.Path != "opencode.json" {
		t.Fatalf("wire opencode response: %+v", out)
	}
	cfg, err := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if err != nil {
		t.Fatalf("opencode.json not written into repo dir: %v", err)
	}
	if !strings.Contains(string(cfg), `"pact"`) {
		t.Fatalf("opencode.json missing pact server:\n%s", cfg)
	}
}

// TestWirePOSTDesktopGlobal: POST a desktop kind writes its machine-global file
// under a faked HOME and the response flags global:true.
func TestWirePOSTDesktopGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "")
	resp := postJSON(t, ts.URL+"/api/projects/pactify/wiring/claude-desktop", map[string]any{"seat": "opencode", "roles": "worker"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wire claude-desktop: want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	var out struct {
		Wrote   bool   `json:"wrote"`
		Global  bool   `json:"global"`
		Path    string `json:"path"`
		DocOnly bool   `json:"docOnly"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if !out.Global {
		t.Fatalf("claude-desktop must flag global: %+v", out)
	}
	cfg := filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("desktop config not written under fake HOME: %v", err)
	}
	if !strings.Contains(string(b), `"pact"`) {
		t.Fatalf("desktop config missing pact server:\n%s", b)
	}
}

// TestWirePOSTCodexDocOnly: POST codex-cli returns docOnly:true and a snippet
// carrying the TOML table header; no config file is written.
func TestWirePOSTCodexDocOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "")
	resp := postJSON(t, ts.URL+"/api/projects/pactify/wiring/codex-cli", map[string]any{"seat": "opencode", "roles": "worker"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wire codex-cli: want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	var out struct {
		Wrote   bool   `json:"wrote"`
		DocOnly bool   `json:"docOnly"`
		Snippet string `json:"snippet"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if !out.DocOnly {
		t.Fatalf("codex-cli must be docOnly: %+v", out)
	}
	if !strings.Contains(out.Snippet, "[mcp_servers.pact]") {
		t.Fatalf("codex snippet must carry the TOML table:\n%s", out.Snippet)
	}
	if _, err := os.Stat(filepath.Join(dir, ".codex", "config.toml")); err == nil {
		t.Fatal("codex config.toml must not be written (doc-only)")
	}
}

// TestWirePOSTBadInputs: unknown kind, bad seat slug, and empty roles each 400.
func TestWirePOSTBadInputs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "")

	// unknown kind → 400 listing supported kinds.
	resp := postJSON(t, ts.URL+"/api/projects/pactify/wiring/nope", map[string]any{"seat": "opencode", "roles": "worker"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown kind: want 400 got %d", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "opencode") {
		t.Fatalf("unknown-kind error should list kinds: %q", msg)
	}

	// bad seat slug → 400.
	resp = postJSON(t, ts.URL+"/api/projects/pactify/wiring/opencode", map[string]any{"seat": "Not A Slug!", "roles": "worker"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad seat: want 400 got %d", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "slug") {
		t.Fatalf("bad-seat error should mention slug: %q", msg)
	}

	// empty roles → 400.
	resp = postJSON(t, ts.URL+"/api/projects/pactify/wiring/opencode", map[string]any{"seat": "opencode", "roles": ""})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty roles: want 400 got %d", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "roles") {
		t.Fatalf("empty-roles error should mention roles: %q", msg)
	}

	// unknown project → 404.
	resp = postJSON(t, ts.URL+"/api/projects/ghost/wiring/opencode", map[string]any{"seat": "opencode", "roles": "worker"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown project: want 404 got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
