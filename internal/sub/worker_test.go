package sub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

type fakeSession struct {
	events    chan cockpit.Event
	approvals chan cockpit.ApprovalRequest
	script    func(*fakeSession)
	info      cockpit.SessionInfo
	canceled  atomic.Bool
	closed    atomic.Bool
}

func (s *fakeSession) Prompt(ctx context.Context, m cockpit.UserMessage) error {
	go s.script(s)
	return nil
}
func (s *fakeSession) Interrupt(context.Context) error {
	s.canceled.Store(true)
	s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "interrupted"}
	return nil
}
func (s *fakeSession) Events() <-chan cockpit.Event              { return s.events }
func (s *fakeSession) Approvals() <-chan cockpit.ApprovalRequest { return s.approvals }
func (s *fakeSession) ThreadID() string                          { return "th-fake" }
func (s *fakeSession) Close() error                              { s.closed.Store(true); return nil }
func (s *fakeSession) Info() cockpit.SessionInfo                 { return s.info }

type fakeBackend struct {
	sess     *fakeSession
	gotOpts  cockpit.StartOpts
	resumed  string
	startErr error
}

func (b *fakeBackend) Start(_ context.Context, o cockpit.StartOpts) (cockpit.Session, error) {
	b.gotOpts = o
	return b.sess, b.startErr
}
func (b *fakeBackend) Resume(_ context.Context, id string) (cockpit.Session, error) {
	b.resumed = id
	return b.sess, b.startErr
}
func (b *fakeBackend) ResumeWith(_ context.Context, id string, o cockpit.StartOpts) (cockpit.Session, error) {
	b.resumed = id
	b.gotOpts = o
	return b.sess, b.startErr
}
func newFakeHarness(b *fakeBackend) Harness {
	return Harness{Name: "fake", Kind: "codex-cli", Backend: func() cockpit.Backend { return b }, Version: func(context.Context) (string, error) { return "9.9.9", nil }, Verify: func(string, cockpit.SessionInfo) (string, int, int, bool) { return "", 0, 0, false }}
}
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "base"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	return dir
}
func workerFixture(t *testing.T, access string) (Store, *fakeSession, *fakeBackend) {
	t.Helper()
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("KIMI_SHARE_DIR", t.TempDir())
	s := &fakeSession{events: make(chan cockpit.Event, 128), approvals: make(chan cockpit.ApprovalRequest, 4)}
	b := &fakeBackend{sess: s}
	store := Store{Root: t.TempDir()}
	_, err := store.Create("j1", Request{Harness: "fake", Access: access, Cwd: gitRepo(t), Brief: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	return store, s, b
}
func contains(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
func TestRunJobWriteSucceeds(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	st, _ := store.Load("j1")
	st.Request.Model = "gpt-6-luna"
	st.Request.Effort = "low"
	_ = store.Save(st)
	s.info.Model = "gpt-6-luna"
	s.script = func(s *fakeSession) {
		_ = os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("hi\n"), 0600)
		s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 100, OutputTokens: 5}}
		s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "WROTE"}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusSucceeded || r.FinalText != "WROTE" || r.ModelActual != "gpt-6-luna" || r.CLIVersion != "9.9.9" || r.InputTokens != 100 || r.OutputTokens != 5 || r.ModelVerified {
		t.Fatalf("result=%+v", r)
	}
	if b.gotOpts.Access != cockpit.AccessWrite || b.gotOpts.Model != "gpt-6-luna" || b.gotOpts.Effort != "low" || b.gotOpts.RepoDir != st.Request.Cwd || b.gotOpts.Stderr == nil || !contains(b.gotOpts.Env, "ANTHROPIC_API_KEY=") {
		t.Fatalf("opts=%+v", b.gotOpts)
	}
	if len(r.Diff.Files) != 1 || r.Diff.Files[0].Path != "out.txt" || !r.Diff.Files[0].New {
		t.Fatalf("diff=%+v", r.Diff)
	}
	saved, _ := store.Load("j1")
	if saved.Status != StatusSucceeded || saved.PID != os.Getpid() || saved.FinishedAt.IsZero() || !s.closed.Load() {
		t.Fatalf("state=%+v", saved)
	}
	data, err := os.ReadFile(store.ResultPath("j1"))
	var persisted Result
	if err != nil || json.Unmarshal(data, &persisted) != nil || persisted.FinalText != "WROTE" {
		t.Fatalf("result file=%s err=%v", data, err)
	}
	data, err = os.ReadFile(store.LogPath("j1"))
	if err != nil || !strings.Contains(string(data), `"text":"WROTE"`) {
		t.Fatalf("log=%s err=%v", data, err)
	}
}
func TestRunJobApprovals(t *testing.T) {
	for _, access := range []string{"write", "read-only"} {
		t.Run(access, func(t *testing.T) {
			store, s, b := workerFixture(t, access)
			decided := make(chan cockpit.Decision, 1)
			s.script = func(s *fakeSession) {
				s.approvals <- cockpit.ApprovalRequest{Kind: "permission", ToolName: "ExitPlanMode", Respond: func(d cockpit.Decision) error {
					decided <- d
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
					return nil
				}}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := RunJob(ctx, store, "j1", newFakeHarness(b), WorkerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			want := cockpit.DecisionAllow
			if access == "read-only" {
				want = cockpit.DecisionDeny
			}
			select {
			case got := <-decided:
				if got != want {
					t.Fatalf("decision=%q", got)
				}
			default:
				t.Fatal("approval not answered")
			}
			if b.gotOpts.Access != cockpit.Access(access) {
				t.Fatalf("access=%q", b.gotOpts.Access)
			}
		})
	}
}
func TestRunJobToolCallLimitInterrupts(t *testing.T) {
	store, s, b := workerFixture(t, "read-only")
	s.script = func(s *fakeSession) {
		for i := 0; i < 50; i++ {
			s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start", Name: "ExitPlanMode"}}
		}
	}
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{ToolCallLimit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !s.canceled.Load() || r.Status != StatusFailed || !strings.HasPrefix(r.Markdown(), "⚠️") || !contains(r.Warnings, "工具调用超限") {
		t.Fatalf("result=%+v", r)
	}
}
func TestRunJobModelVerificationAndResume(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	st, _ := store.Load("j1")
	st.Request.Model = "requested"
	st.Request.Resume = "old-thread"
	_ = store.Save(st)
	s.info.Model = "advertised"
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	h := newFakeHarness(b)
	h.Kind = "kimi-cli"
	h.Verify = func(sid string, info cockpit.SessionInfo) (string, int, int, bool) {
		if sid != "th-fake" || info.Model != "advertised" {
			t.Errorf("bad verification inputs")
		}
		return "actual", 321, 12, true
	}
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.ModelVerified || r.ModelActual != "actual" || r.InputTokens != 321 || r.OutputTokens != 12 || !contains(r.Warnings, "实际模型与请求不一致") || b.resumed != "old-thread" || b.gotOpts.Access != cockpit.AccessWrite {
		t.Fatalf("result=%+v backend=%+v", r, b)
	}
}
func TestRunJobIdleTimeoutAndCancel(t *testing.T) {
	for _, cancelJob := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "cancel"}[cancelJob], func(t *testing.T) {
			store, s, b := workerFixture(t, "write")
			s.script = func(*fakeSession) {}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg := WorkerConfig{IdleTimeout: 50 * time.Millisecond}
			if cancelJob {
				timer := time.AfterFunc(50*time.Millisecond, cancel)
				defer timer.Stop()
				cfg.IdleTimeout = time.Minute
			}
			r, err := RunJob(ctx, store, "j1", newFakeHarness(b), cfg)
			if err != nil {
				t.Fatal(err)
			}
			want := StatusFailed
			if cancelJob {
				want = StatusCanceled
			}
			if r.Status != want || !s.canceled.Load() {
				t.Fatalf("result=%+v", r)
			}
			if !cancelJob && !contains(r.Warnings, "空闲超时") {
				t.Fatalf("warnings=%v", r.Warnings)
			}
		})
	}
}
func TestRunJobFailuresPersist(t *testing.T) {
	for _, mode := range []string{"start", "turn", "closed"} {
		t.Run(mode, func(t *testing.T) {
			store, s, b := workerFixture(t, "write")
			if mode == "start" {
				b.startErr = errors.New("requested model unavailable; available: other")
			}
			s.script = func(s *fakeSession) {
				if mode == "closed" {
					close(s.events)
				} else {
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "backend failed"}
				}
			}
			r, _ := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
			st, _ := store.Load("j1")
			if r.Status != StatusFailed || st.Status != StatusFailed || len(r.Warnings) == 0 {
				t.Fatalf("result=%+v state=%+v", r, st)
			}
			if _, err := os.Stat(store.ResultPath("j1")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestSubHarnessRegistry(t *testing.T) {
	t.Setenv("PACTIFY_SUB_FAKE", "")
	if _, ok := Lookup("fake"); ok {
		t.Fatal("fake enabled by default")
	}
	if got := strings.Join(Names(), ","); got != "codex,kimi" {
		t.Fatalf("names=%s", got)
	}
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	if _, ok := Lookup("fake"); !ok {
		t.Fatal("fake unavailable for CLI integration tests")
	}
}

func TestSubKimiVerifyUsesIsolatedHome(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	session := filepath.Join(home, "sessions", "test")
	if err := os.MkdirAll(filepath.Join(session, "agents", "main"), 0700); err != nil {
		t.Fatal(err)
	}
	index, _ := json.Marshal(map[string]string{"sessionId": "isolated-session", "sessionDir": session})
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), append(index, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session, "agents", "main", "wire.jsonl"), []byte("{\"type\":\"llm.request\",\"model\":\"kimi-for-coding\",\"modelAlias\":\"kimi-code/kimi-for-coding\"}\n{\"type\":\"usage.record\",\"usageScope\":\"turn\",\"usage\":{\"inputOther\":10,\"inputCacheRead\":100,\"output\":4}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h, _ := Lookup("kimi")
	model, in, out, ok := h.Verify("isolated-session", cockpit.SessionInfo{})
	if !ok || model != "kimi-code/kimi-for-coding" || in != 110 || out != 4 {
		t.Fatalf("verify=%q,%d,%d,%v", model, in, out, ok)
	}
}
