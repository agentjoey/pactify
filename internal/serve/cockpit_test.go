package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/registry"
)

// testCockpitBackend returns a fixed session so tests can drive it with
// Emit/EmitApproval without reaching for real agent binaries.
type testCockpitBackend struct {
	sess cockpit.Session
}

func (b *testCockpitBackend) Start(ctx context.Context, opts cockpit.StartOpts) (cockpit.Session, error) {
	return b.sess, nil
}

func (b *testCockpitBackend) Resume(ctx context.Context, threadID string) (cockpit.Session, error) {
	return b.sess, nil
}

func newCockpitTestServer(t *testing.T) (*Server, *cockpit.FakeSession, string) {
	t.Helper()
	dir := t.TempDir()
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	fake := cockpit.NewFakeSession("claude")
	srv.cockpit = cockpit.NewManager(t.TempDir(), func(k cockpit.SessionKey) (cockpit.Backend, error) {
		return &testCockpitBackend{sess: fake}, nil
	})
	return srv, fake, dir
}

func postCockpit(t *testing.T, h http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(http.MethodPost, path, r)
	req.Header.Set("Content-Type", "application/json")
	// No Origin header → writeGuard treats this as a CLI/script request and allows.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func getCockpit(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestCockpitPromptCreatesSession(t *testing.T) {
	srv, fake, _ := newCockpitTestServer(t)
	h := srv.Handler()

	rr := postCockpit(t, h, "/api/projects/p/cockpit/prompt", map[string]string{
		"seat": "claude",
		"text": "hi",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("prompt: status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp cockpitPromptResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.ThreadID == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	if len(fake.Prompts) != 1 || fake.Prompts[0].Text != "hi" {
		t.Fatalf("expected prompt 'hi', got %+v", fake.Prompts)
	}
}

func TestCockpitStatusAndPermission(t *testing.T) {
	srv, fake, _ := newCockpitTestServer(t)
	h := srv.Handler()

	// Create the session first.
	postCockpit(t, h, "/api/projects/p/cockpit/prompt", map[string]string{
		"seat": "claude",
		"text": "do it",
	})

	// No approvals yet.
	rr := getCockpit(t, h, "/api/projects/p/cockpit/status?seat=claude")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rr.Code, rr.Body.String())
	}
	var st cockpitStatusDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.ThreadID == "" {
		t.Fatal("expected thread id")
	}
	if len(st.Pending) != 0 {
		t.Fatalf("expected no pending approvals, got %+v", st.Pending)
	}

	// Emit an approval request and verify it appears in status.
	var gotDecision cockpit.Decision
	wantRawInput := json.RawMessage(`{"path":"/etc","recursive":false}`)
	fake.EmitApproval(cockpit.ApprovalRequest{
		Kind:     "command",
		ToolName: "ls",
		RawInput: wantRawInput,
		Respond: func(d cockpit.Decision) error {
			gotDecision = d
			return nil
		},
	})
	// Give the approval pump a moment to assign an id.
	time.Sleep(50 * time.Millisecond)

	rr = getCockpit(t, h, "/api/projects/p/cockpit/status?seat=claude")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Pending) != 1 {
		t.Fatalf("expected 1 pending approval, got %+v", st.Pending)
	}
	if st.Pending[0].ToolName != "ls" {
		t.Fatalf("unexpected pending item: %+v", st.Pending[0])
	}
	if string(st.Pending[0].RawInput) != string(wantRawInput) {
		t.Fatalf("rawInput mismatch: got %q, want %q", st.Pending[0].RawInput, wantRawInput)
	}

	// Respond to the approval.
	rr = postCockpit(t, h, "/api/projects/p/cockpit/permission", map[string]string{
		"seat":       "claude",
		"approvalId": st.Pending[0].ID,
		"decision":   "allow",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("permission: %d %s", rr.Code, rr.Body.String())
	}
	if gotDecision != cockpit.DecisionAllow {
		t.Fatalf("expected decision allow, got %q", gotDecision)
	}

	// Pending list should now be empty.
	rr = getCockpit(t, h, "/api/projects/p/cockpit/status?seat=claude")
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Pending) != 0 {
		t.Fatalf("expected pending to clear, got %+v", st.Pending)
	}
}

func TestCockpitCancel(t *testing.T) {
	srv, fake, _ := newCockpitTestServer(t)
	h := srv.Handler()

	postCockpit(t, h, "/api/projects/p/cockpit/prompt", map[string]string{
		"seat": "claude",
		"text": "go",
	})

	rr := postCockpit(t, h, "/api/projects/p/cockpit/cancel", map[string]string{"seat": "claude"})
	if rr.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", rr.Code, rr.Body.String())
	}

	if !fake.InterruptCalled {
		t.Fatal("expected Interrupt to be called")
	}
}

func TestCockpitStreamReplaysHistory(t *testing.T) {
	srv, fake, _ := newCockpitTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Create session and emit an event before opening the stream.
	postCockpit(t, srv.Handler(), "/api/projects/p/cockpit/prompt", map[string]string{
		"seat": "claude",
		"text": "stream me",
	})
	fake.Emit(cockpit.Event{Kind: cockpit.EventMessage, Text: "hello"})

	// Wait until the event is persisted to the session jsonl.
	key := cockpit.SessionKey{Project: "p", Seat: "claude"}
	cs, ok := srv.cockpit.Get(key)
	if !ok {
		t.Fatal("session not found")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		hist, err := cs.History()
		if err == nil && len(hist) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("event was not persisted to history")
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp, err := http.Get(ts.URL + "/api/projects/p/cockpit/stream?seat=claude")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status %d", resp.StatusCode)
	}

	got := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data:") && strings.Contains(line, "hello") {
				got <- line
				return
			}
		}
	}()

	select {
	case line := <-got:
		// Lowercase json keys are the contract the dashboard CockpitPanel reads
		// (ev.kind / ev.text). Capitalized keys would render every event as an
		// empty row.
		if !strings.Contains(line, `"text":"hello"`) || !strings.Contains(line, `"kind":"message"`) {
			t.Fatalf("expected lowercase event data line with hello, got %q", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive replayed event within timeout")
	}
}

func TestCockpitUnknownProject(t *testing.T) {
	srv, _, _ := newCockpitTestServer(t)
	h := srv.Handler()

	rr := postCockpit(t, h, "/api/projects/unknown/cockpit/prompt", map[string]string{
		"seat": "claude",
		"text": "hi",
	})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d %s", rr.Code, rr.Body.String())
	}

	rr = getCockpit(t, h, "/api/projects/unknown/cockpit/status?seat=claude")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d %s", rr.Code, rr.Body.String())
	}
}

func TestBackendForKeyRejectsNonDeepIntegration(t *testing.T) {
	dir := t.TempDir()
	s := New([]registry.Project{{Name: "p", Path: dir}})
	_, err := s.backendForKey(cockpit.SessionKey{Project: "p", Seat: "foo"})
	if err == nil {
		t.Fatal("expected error for non-deep-integration seat")
	}
	if !strings.Contains(err.Error(), "not deep-integration") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCockpitAuditSink(t *testing.T) {
	dir := t.TempDir()
	s := New([]registry.Project{{Name: "p", Path: dir}})

	auditHome := t.TempDir()
	t.Setenv("PACTIFY_HOME", auditHome)

	key := cockpit.SessionKey{Project: "p", Seat: "claude"}
	s.cockpitAudit(key, cockpit.AuditEvent{
		Tool:    "Bash",
		Summary: "Bash start",
		Risk:    "exec",
	})

	day := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(auditHome, ".pactify", "audit", "p", day+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading audit file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected audit record to be written")
	}
	line := string(data)
	for _, want := range []string{`"project":"p"`, `"repo":"` + dir + `"`, `"seat":"claude"`, `"kind":"cockpit"`, `"tool":"Bash"`, `"summary":"Bash start"`, `"risk":"exec"`, `"session":"p/claude"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("audit line missing %q: %s", want, line)
		}
	}
}

func TestBackendForKeySelectsByKind(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".pact"), 0o755)
	logPath := filepath.Join(dir, ".pact", "log.jsonl")
	initLine := `{"event_type":"init","agent_id":"orchestrator","payload":{"project":"p","seats":[{"id":"claude","kind":"claude-code"},{"id":"coder","kind":"codex-cli"},{"id":"other","kind":"opencode"}]}}`
	if err := os.WriteFile(logPath, []byte(initLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New([]registry.Project{{Name: "p", Path: dir}})

	b, err := s.backendForKey(cockpit.SessionKey{Project: "p", Seat: "claude"})
	if err != nil {
		t.Fatalf("claude-code seat: %v", err)
	}
	if _, ok := b.(interface {
		Start(context.Context, cockpit.StartOpts) (cockpit.Session, error)
	}); !ok {
		t.Fatalf("expected backend for claude-code, got %T", b)
	}

	b, err = s.backendForKey(cockpit.SessionKey{Project: "p", Seat: "coder"})
	if err != nil {
		t.Fatalf("codex-cli seat: %v", err)
	}
	if _, ok := b.(interface {
		Start(context.Context, cockpit.StartOpts) (cockpit.Session, error)
	}); !ok {
		t.Fatalf("expected backend for codex-cli, got %T", b)
	}

	_, err = s.backendForKey(cockpit.SessionKey{Project: "p", Seat: "other"})
	if err == nil {
		t.Fatal("expected error for opencode seat")
	}
}
