package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	return cockpit.NewFakeSession(threadID), nil
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

func TestCockpitPromptRateLimitReturns429(t *testing.T) {
	srv, _, _ := newCockpitTestServer(t)
	h := srv.Handler()

	for i := 0; i < 10; i++ {
		rr := postCockpit(t, h, "/api/projects/p/cockpit/prompt", map[string]string{
			"seat": "claude",
			"text": fmt.Sprintf("prompt %d", i),
		})
		if rr.Code != http.StatusOK {
			t.Fatalf("prompt %d: status = %d, body = %s", i, rr.Code, rr.Body.String())
		}
	}

	rr := postCockpit(t, h, "/api/projects/p/cockpit/prompt", map[string]string{
		"seat": "claude",
		"text": "over limit",
	})
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("11th prompt: status = %d, want 429, body = %s", rr.Code, rr.Body.String())
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

func TestCockpitStatusPendingRisk(t *testing.T) {
	srv, fake, _ := newCockpitTestServer(t)
	h := srv.Handler()

	postCockpit(t, h, "/api/projects/p/cockpit/prompt", map[string]string{
		"seat": "claude",
		"text": "do it",
	})

	fake.EmitApproval(cockpit.ApprovalRequest{
		Kind:     "command",
		ToolName: "bash",
		RawInput: json.RawMessage(`{"command":"echo hi"}`),
		Respond:  func(_ cockpit.Decision) error { return nil },
	})
	time.Sleep(50 * time.Millisecond)

	rr := getCockpit(t, h, "/api/projects/p/cockpit/status?seat=claude")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rr.Code, rr.Body.String())
	}
	var st cockpitStatusDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Pending) != 1 {
		t.Fatalf("expected 1 pending approval, got %+v", st.Pending)
	}
	if st.Pending[0].Risk != "exec" {
		t.Fatalf("expected risk exec for bash approval, got %q", st.Pending[0].Risk)
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

func TestCockpitCapableKindIncludesOpencode(t *testing.T) {
	if !cockpitCapableKind("opencode") {
		t.Fatal("opencode must be a cockpit-capable kind")
	}
	if cockpitCapableKind("unknown-kind") {
		t.Fatal("unknown kind must not be cockpit-capable")
	}
}

func TestCockpitStatusResumable(t *testing.T) {
	srv, _, dir := newCockpitTestServer(t)
	h := srv.Handler()

	// Register the seat as a deep-integration kind so the capability pre-flight
	// reports capable when there is no live session.
	_ = os.MkdirAll(filepath.Join(dir, ".pact"), 0o755)
	logPath := filepath.Join(dir, ".pact", "log.jsonl")
	initLine := `{"event_type":"init","agent_id":"orchestrator","payload":{"project":"p","seats":[{"id":"claude","kind":"claude-code"}]}}`
	if err := os.WriteFile(logPath, []byte(initLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	key := cockpit.SessionKey{Project: "p", Seat: "claude"}
	srv.cockpit.NoteThread(key, "stored-thread")

	rr := getCockpit(t, h, "/api/projects/p/cockpit/status?seat=claude")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rr.Code, rr.Body.String())
	}
	var st cockpitStatusDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Capable {
		t.Fatal("expected capable true")
	}
	if !st.Resumable {
		t.Fatal("expected resumable true when a thread is stored but no live session")
	}
}

func TestCockpitResumeEndpoint(t *testing.T) {
	srv, _, _ := newCockpitTestServer(t)
	h := srv.Handler()

	key := cockpit.SessionKey{Project: "p", Seat: "claude"}
	srv.cockpit.NoteThread(key, "stored-thread")

	rr := postCockpit(t, h, "/api/projects/p/cockpit/resume", map[string]string{"seat": "claude"})
	if rr.Code != http.StatusOK {
		t.Fatalf("resume: %d %s", rr.Code, rr.Body.String())
	}
	var resp cockpitResumeResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.ThreadID != "stored-thread" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	// A session now exists.
	if _, ok := srv.cockpit.Get(key); !ok {
		t.Fatal("expected live session after resume")
	}
}

func TestCockpitResumeNothingStored(t *testing.T) {
	srv, _, _ := newCockpitTestServer(t)
	h := srv.Handler()

	rr := postCockpit(t, h, "/api/projects/p/cockpit/resume", map[string]string{"seat": "claude"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", rr.Code, rr.Body.String())
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

	b, err = s.backendForKey(cockpit.SessionKey{Project: "p", Seat: "other"})
	if err != nil {
		t.Fatalf("opencode seat: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend for opencode seat")
	}
}

// Status must report capability so the panel can gate input BEFORE the user
// types: a live session is capable by definition; with no session it falls back
// to the seat's roster kind (empty kind => incapable, with a reason). The
// original implementation declared Capable/Reason on the DTO but never set them
// — which also mis-reported capable seats (zero value false) as incapable.
func TestCockpitStatusCapability(t *testing.T) {
	srv, _, _ := newCockpitTestServer(t)
	h := srv.Handler()

	// No session + no roster (empty seatKind) => incapable with a reason.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/projects/p/cockpit/status?seat=ghost", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var st struct {
		Capable bool   `json:"capable"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Capable {
		t.Fatal("kindless seat must be incapable")
	}
	if st.Reason == "" {
		t.Fatal("incapable must carry a human-readable reason")
	}

	// A live session makes the seat capable regardless of roster kind.
	rr2 := postCockpit(t, h, "/api/projects/p/cockpit/prompt", map[string]string{"seat": "orch", "text": "hi"})
	if rr2.Code != http.StatusOK {
		t.Fatalf("prompt = %d: %s", rr2.Code, rr2.Body.String())
	}
	rr3 := httptest.NewRecorder()
	h.ServeHTTP(rr3, httptest.NewRequest("GET", "/api/projects/p/cockpit/status?seat=orch", nil))
	if err := json.Unmarshal(rr3.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Capable {
		t.Fatal("seat with a live session must be capable")
	}
}
