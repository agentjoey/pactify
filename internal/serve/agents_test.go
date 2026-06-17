package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentsGET(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	srv := New(nil)
	ts := newTestServer(t, srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/agents")
	if err != nil {
		t.Fatalf("GET agents: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d", resp.StatusCode)
	}
	var items []struct {
		Kind       string `json:"kind"`
		Installed  bool   `json:"installed"`
		Detail     string `json:"detail"`
		Registered bool   `json:"registered"`
		Label      string `json:"label,omitempty"`
	}
	json.NewDecoder(resp.Body).Decode(&items)
	if len(items) == 0 {
		t.Fatal("GET agents returned empty list")
	}
	// Every scanned kind must appear.
	hasOpenCode := false
	for _, it := range items {
		if it.Kind == "opencode" {
			hasOpenCode = true
			if it.Registered {
				t.Fatalf("opencode registered should be false before any register: %+v", it)
			}
		}
	}
	if !hasOpenCode {
		t.Fatalf("GET agents missing opencode: %+v", items)
	}
}

func TestAgentsRegisterAndUnregister(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	srv := New(nil)
	srv.SetSeat("test")
	ts := newTestServer(t, srv)
	defer ts.Close()

	// POST register opencode with a label.
	resp := postJSON(t, ts.URL+"/api/agents/opencode/register", map[string]any{"label": "My OpenCode"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register want 200 got %d (%s)", resp.StatusCode, errBody(t, resp))
	}
	resp.Body.Close()

	// GET: now opencode must be registered=true with the label.
	respG, err := http.Get(ts.URL + "/api/agents")
	if err != nil {
		t.Fatal(err)
	}
	defer respG.Body.Close()
	var items []struct {
		Kind       string `json:"kind"`
		Installed  bool   `json:"installed"`
		Detail     string `json:"detail"`
		Registered bool   `json:"registered"`
		Label      string `json:"label,omitempty"`
	}
	json.NewDecoder(respG.Body).Decode(&items)
	var got *struct {
		Kind       string `json:"kind"`
		Installed  bool   `json:"installed"`
		Detail     string `json:"detail"`
		Registered bool   `json:"registered"`
		Label      string `json:"label,omitempty"`
	}
	for i := range items {
		if items[i].Kind == "opencode" {
			got = &items[i]
			break
		}
	}
	if got == nil {
		t.Fatal("opencode not in agents list after register")
	}
	if !got.Registered {
		t.Fatal("opencode registered should be true after register")
	}
	if got.Label != "My OpenCode" {
		t.Fatalf("opencode label = %q, want My OpenCode", got.Label)
	}

	// DELETE unregister opencode.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/agents/opencode/register", nil)
	respDel, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if respDel.StatusCode != http.StatusOK {
		t.Fatalf("unregister want 200 got %d", respDel.StatusCode)
	}
	respDel.Body.Close()

	// GET: opencode registered must be false again.
	respG2, err := http.Get(ts.URL + "/api/agents")
	if err != nil {
		t.Fatal(err)
	}
	defer respG2.Body.Close()
	json.NewDecoder(respG2.Body).Decode(&items)
	for i := range items {
		if items[i].Kind == "opencode" && items[i].Registered {
			t.Fatal("opencode registered should be false after unregister")
		}
	}
}

func TestAgentsUnknownKind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	srv := New(nil)
	srv.SetSeat("test")
	ts := newTestServer(t, srv)
	defer ts.Close()

	// POST unknown kind → 400.
	resp := postJSON(t, ts.URL+"/api/agents/nope/register", map[string]any{"label": "x"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown kind: want 400 got %d", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "opencode") {
		t.Fatalf("unknown-kind error should list kinds: %q", msg)
	}
	resp.Body.Close()

	// DELETE unknown kind → 400.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/agents/nope/register", nil)
	respDel, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if respDel.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown kind DELETE: want 400 got %d", respDel.StatusCode)
	}
	respDel.Body.Close()
}

func TestAgentConfigGetUnregistered(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	srv := New(nil)
	ts := newTestServer(t, srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/agents/opencode/config")
	if err != nil {
		t.Fatalf("GET config: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d", resp.StatusCode)
	}
	var dto struct {
		Kind            string   `json:"kind"`
		Registered      bool     `json:"registered"`
		Drivable        bool     `json:"drivable"`
		Model           string   `json:"model"`
		AllowedTools    []string `json:"allowed_tools"`
		Restricted      bool     `json:"restricted"`
		EffModel        string   `json:"effective_model"`
		EffScoped       bool     `json:"effective_scoped"`
		CandidateModels []string `json:"candidate_models"`
	}
	json.NewDecoder(resp.Body).Decode(&dto)
	if dto.Registered {
		t.Fatal("expected registered=false for unregistered opencode")
	}
	if !dto.Drivable {
		t.Fatal("expected drivable=true for opencode")
	}
	if dto.EffModel == "" {
		t.Fatal("expected non-empty effective_model (default)")
	}
	// candidate_models drives the config model dropdown; opencode curates its
	// default model.
	if len(dto.CandidateModels) == 0 || dto.CandidateModels[0] != "deepseek/deepseek-v4-pro" {
		t.Fatalf("expected candidate_models to lead with the opencode default, got %v", dto.CandidateModels)
	}
}

func TestAgentConfigGetUnknownKind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	srv := New(nil)
	ts := newTestServer(t, srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/agents/nope/config")
	if err != nil {
		t.Fatalf("GET config unknown: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown kind: want 404 got %d", resp.StatusCode)
	}
}

func TestAgentConfigSetAndGet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	srv := New(nil)
	srv.SetSeat("test")
	ts := newTestServer(t, srv)
	defer ts.Close()

	// First, register opencode.
	respReg := postJSON(t, ts.URL+"/api/agents/opencode/register", map[string]any{"label": "oc"})
	if respReg.StatusCode != http.StatusOK {
		t.Fatalf("register: want 200 got %d (%s)", respReg.StatusCode, errBody(t, respReg))
	}
	respReg.Body.Close()

	// POST config.
	respSet := postJSON(t, ts.URL+"/api/agents/opencode/config", map[string]any{
		"model":         "deepseek/custom",
		"restricted":    true,
		"allowed_tools": []string{"Read", "Edit"},
	})
	if respSet.StatusCode != http.StatusOK {
		t.Fatalf("config set: want 200 got %d (%s)", respSet.StatusCode, errBody(t, respSet))
	}
	var dto struct {
		Kind         string   `json:"kind"`
		Registered   bool     `json:"registered"`
		Drivable     bool     `json:"drivable"`
		Model        string   `json:"model"`
		AllowedTools []string `json:"allowed_tools"`
		Restricted   bool     `json:"restricted"`
		EffModel     string   `json:"effective_model"`
		EffScoped    bool     `json:"effective_scoped"`
	}
	json.NewDecoder(respSet.Body).Decode(&dto)
	respSet.Body.Close()

	if dto.Model != "deepseek/custom" {
		t.Fatalf("model = %q, want deepseek/custom", dto.Model)
	}
	if !dto.Restricted {
		t.Fatal("expected restricted=true")
	}
	if dto.EffModel != "deepseek/custom" {
		t.Fatalf("effective_model = %q, want deepseek/custom", dto.EffModel)
	}
}

func TestAgentConfigSetUnregistered(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	srv := New(nil)
	srv.SetSeat("test")
	ts := newTestServer(t, srv)
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/agents/opencode/config", map[string]any{
		"model": "deepseek/custom",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unregistered config: want 400 got %d", resp.StatusCode)
	}
	if msg := errBody(t, resp); !strings.Contains(msg, "register") {
		t.Fatalf("expected 'register' in error: %q", msg)
	}
	resp.Body.Close()
}

// newTestServer is a thin wrapper returning an httptest.Server.
func newTestServer(t *testing.T, srv *Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}
