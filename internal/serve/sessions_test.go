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

	// claude-code has no verified session command → supported=false, no output, no run.
	sessionRunFn = func(_ string, _ ...string) (string, error) { t.Fatal("runner should not be called"); return "", nil }
	r2 := httptest.NewRequest("GET", "/api/agents/claude-code/sessions", nil)
	r2.SetPathValue("kind", "claude-code")
	w2 := httptest.NewRecorder()
	s.handleSessionsList(w2, r2)
	var resp2 sessionsListResp
	json.NewDecoder(w2.Body).Decode(&resp2)
	if resp2.Supported {
		t.Fatalf("claude-code should be unsupported: %+v", resp2)
	}

	// opencode IS supported now (session list + delete-by-id) → output flows through.
	sessionRunFn = func(_ string, _ ...string) (string, error) { return "ses_1  pact:dev\n", nil }
	r3 := httptest.NewRequest("GET", "/api/agents/opencode/sessions", nil)
	r3.SetPathValue("kind", "opencode")
	w3 := httptest.NewRecorder()
	s.handleSessionsList(w3, r3)
	var resp3 sessionsListResp
	json.NewDecoder(w3.Body).Decode(&resp3)
	if !resp3.Supported || resp3.Output != "ses_1  pact:dev\n" {
		t.Fatalf("opencode resp = %+v, want supported with output", resp3)
	}
}

func TestHandleSessionsPrune_UnsupportedIsGracefulNoop(t *testing.T) {
	orig := sessionRunFn
	t.Cleanup(func() { sessionRunFn = orig })
	// claude-code has no session spec → prune is a skipped no-op, never an error or
	// a process spawn. (gemini-cli now supports index-prune, so it is no longer the
	// "unsupported" example.)
	sessionRunFn = func(_ string, _ ...string) (string, error) { t.Fatal("runner should not be called"); return "", nil }

	s := &Server{seat: "test"}
	r := httptest.NewRequest("POST", "/api/agents/claude-code/sessions/prune", nil)
	r.SetPathValue("kind", "claude-code")
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
