package sub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
)

// Contract: every harness pactify can delegate to must keep its sessions out of the
// user's own session lists — either the backend hides them itself (codex archives
// the thread on Close) or the harness provides Conceal. A new harness without a
// strategy fails here instead of littering the user's apps (Owner, 2026-10-15).
func TestEveryHarnessHidesItsSessions(t *testing.T) {
	for _, name := range Names() {
		h, _ := Lookup(name)
		if !h.HidesItself && h.Conceal == nil {
			t.Errorf("harness %q has no session-hiding strategy (set HidesItself or Conceal)", name)
		}
	}
}

func TestRunJobConcealsTheSessionAfterTheJob(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "Actual result"}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	var got ConcealContext
	h := newFakeHarness(b)
	h.Conceal = func(c ConcealContext) error { got = c; return nil }
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := store.Load("j1")
	if got.SessionID != "th-fake" || got.Cwd != st.Request.Cwd || !s.closed.Load() {
		t.Fatalf("conceal ctx = %+v (closed=%v)", got, s.closed.Load())
	}
	if r.Status != StatusSucceeded || len(r.Warnings) != 0 {
		t.Fatalf("status=%s warnings=%v", r.Status, r.Warnings)
	}
}

func TestRunJobConcealFailureIsAWarning(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	h := newFakeHarness(b)
	h.Conceal = func(ConcealContext) error { return errors.New("state.json: permission denied") }
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusSucceeded || !contains(r.Warnings, "permission denied") {
		t.Fatalf("status=%s warnings=%v", r.Status, r.Warnings)
	}
}

// kimi: the official server API is the primary path; if it fails, the on-disk
// archive fields still hide the session from the CLI lists, and the summary says
// the web UI may still show it.
func TestConcealKimiFallsBackToFileArchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	dir := filepath.Join(home, "sessions", "wd_repo_1", "session_x")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"id":"session_x","archived":false}`), 0o600)
	_ = os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(`{"sessionId":"session_x","sessionDir":"`+dir+`","workDir":"/repo"}`+"\n"), 0o600)
	restoreAPI, restoreList := kimiServerArchive, kimiListHides
	t.Cleanup(func() { kimiServerArchive, kimiListHides = restoreAPI, restoreList })
	kimiListHides = func(string, string) error { return nil }

	kimiServerArchive = func(context.Context, string) error { return nil }
	if err := concealKimi(ConcealContext{SessionID: "session_x", Cwd: "/repo"}); err != nil {
		t.Fatalf("API path: %v", err)
	}

	kimiServerArchive = func(context.Context, string) error { return errors.New("kimi web did not start") }
	err := concealKimi(ConcealContext{SessionID: "session_x", Cwd: "/repo"})
	if err == nil || !strings.Contains(err.Error(), "kimi web") {
		t.Fatalf("fallback must warn about the web UI: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if !strings.Contains(string(b), `"archived":true`) || !strings.Contains(string(b), "archivedAt") {
		t.Fatalf("file fallback not applied: %s", b)
	}
}
