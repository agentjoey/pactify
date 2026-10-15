package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestRemovedGeminiAPI(t *testing.T) {
	s := &Server{seat: "test"}
	for name, handler := range map[string]http.HandlerFunc{
		"register":   s.handleAgentRegister,
		"config get": s.handleAgentConfigGet,
		"config set": s.handleAgentConfigSet,
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(`{}`))
			r.SetPathValue("kind", "gemini-cli")
			w := httptest.NewRecorder()
			handler(w, r)
			if w.Code < 400 || !strings.Contains(w.Body.String(), "gemini-cli has been removed; use antigravity (agy) instead") {
				t.Fatalf("response = %d %s", w.Code, w.Body.String())
			}
		})
	}
	if cockpitCapableKind("gemini-cli") {
		t.Error("removed cockpit transport is available")
	}
}

func TestRemovedGeminiRosterCockpit(t *testing.T) {
	srv, _, dir := newCockpitTestServer(t)
	defer srv.Stop()
	if err := os.MkdirAll(filepath.Join(dir, ".pact"), 0755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"event_type":"init","agent_id":"orchestrator","payload":{"project":"p","seats":[{"id":"old","kind":"gemini-cli"}]}}`
	if err := os.WriteFile(filepath.Join(dir, ".pact", "log.jsonl"), []byte(legacy+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	const message = "gemini-cli has been removed; use antigravity (agy) instead"
	if _, err := srv.backendForKey(cockpit.SessionKey{Project: "p", Seat: "old"}); err == nil || !strings.Contains(err.Error(), message) {
		t.Errorf("backend error = %v", err)
	}
	rr := getCockpit(t, srv.Handler(), "/api/projects/p/cockpit/status?seat=old")
	var status cockpitStatusDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if rr.Code != http.StatusOK || status.Capable || !strings.Contains(status.Reason, message) {
		t.Fatalf("legacy roster status = %d %+v", rr.Code, status)
	}
}

func TestRemovedGeminiSetupAPI(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	payload, err := json.Marshal(setupApplyReq{Path: dir, Project: "old", Seats: []setupApplySeat{{ID: "w", Roles: []string{"worker"}, Entry: "GEMINI.md", Kind: "gemini-cli"}}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/setup/apply", strings.NewReader(string(payload)))
	w := httptest.NewRecorder()
	s := New(nil)
	s.SetSeat("test")
	defer s.Stop()
	s.handleSetupApply(w, r)
	if w.Code < 400 || !strings.Contains(w.Body.String(), "gemini-cli has been removed; use antigravity (agy) instead") {
		t.Errorf("response = %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".pact")); !os.IsNotExist(err) {
		t.Errorf("setup wrote .pact for removed kind: %v", err)
	}
}
