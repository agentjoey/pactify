package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleSessionsList(t *testing.T) {
	orig := sessionRunFn
	t.Cleanup(func() { sessionRunFn = orig })
	sessionRunFn = func(_ string, _ ...string) (string, error) { return "session-A\nsession-B\n", nil }

	s := &Server{}

	// gemini-cli IS supported → the listing output flows through.
	r := httptest.NewRequest("GET", "/api/agents/gemini-cli/sessions", nil)
	r.SetPathValue("kind", "gemini-cli")
	w := httptest.NewRecorder()
	s.handleSessionsList(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp sessionsListResp
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Supported || resp.Output != "session-A\nsession-B\n" {
		t.Fatalf("resp = %+v, want supported with output", resp)
	}

	// opencode has no session command → supported=false, no output, no run.
	sessionRunFn = func(_ string, _ ...string) (string, error) { t.Fatal("runner should not be called"); return "", nil }
	r2 := httptest.NewRequest("GET", "/api/agents/opencode/sessions", nil)
	r2.SetPathValue("kind", "opencode")
	w2 := httptest.NewRecorder()
	s.handleSessionsList(w2, r2)
	var resp2 sessionsListResp
	json.NewDecoder(w2.Body).Decode(&resp2)
	if resp2.Supported {
		t.Fatalf("opencode should be unsupported: %+v", resp2)
	}
}

func TestHandleSessionsPrune_UnsupportedIsGracefulNoop(t *testing.T) {
	orig := sessionRunFn
	t.Cleanup(func() { sessionRunFn = orig })
	// No kind has a verified prune command yet → prune is a skipped no-op, never
	// an error or a process spawn.
	sessionRunFn = func(_ string, _ ...string) (string, error) { t.Fatal("runner should not be called"); return "", nil }

	s := &Server{}
	r := httptest.NewRequest("POST", "/api/agents/gemini-cli/sessions/prune", nil)
	r.SetPathValue("kind", "gemini-cli")
	w := httptest.NewRecorder()
	s.handleSessionsPrune(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp sessionsPruneResp
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Skipped {
		t.Fatalf("expected skipped no-op, got %+v", resp)
	}
}
