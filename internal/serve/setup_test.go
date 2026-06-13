package serve

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSetupSuggestGET(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	srv := New(nil)
	ts := newTestServer(t, srv)
	defer ts.Close()

	// Empty registry → empty roster (UI prompts to register).
	resp, err := http.Get(ts.URL + "/api/setup/suggest")
	if err != nil {
		t.Fatalf("GET setup/suggest: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d", resp.StatusCode)
	}
	var dto struct {
		Bindings []struct {
			Seat     string   `json:"seat"`
			Kind     string   `json:"kind"`
			Roles    []string `json:"roles"`
			Drivable bool     `json:"drivable"`
		} `json:"bindings"`
		Warnings []string `json:"warnings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(dto.Bindings) != 0 {
		t.Fatalf("empty registry should yield no bindings, got %+v", dto.Bindings)
	}

	// Register two agents → roster proposes a lead + a worker, no gaps.
	postJSON(t, ts.URL+"/api/agents/claude-code/register", map[string]any{})
	postJSON(t, ts.URL+"/api/agents/opencode/register", map[string]any{})

	resp2, _ := http.Get(ts.URL + "/api/setup/suggest")
	defer resp2.Body.Close()
	json.NewDecoder(resp2.Body).Decode(&dto)
	if len(dto.Bindings) != 2 {
		t.Fatalf("want 2 bindings, got %d: %+v", len(dto.Bindings), dto.Bindings)
	}
	if dto.Bindings[0].Kind != "claude-code" || dto.Bindings[0].Roles[0] != "orchestrator" {
		t.Errorf("lead = %+v, want claude-code orchestrator", dto.Bindings[0])
	}
	if len(dto.Warnings) != 0 {
		t.Errorf("a lead+worker roster should have no warnings, got %v", dto.Warnings)
	}
}
