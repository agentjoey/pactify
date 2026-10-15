package sub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	closeErr  error
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
func (s *fakeSession) Close() error                              { s.closed.Store(true); return s.closeErr }
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

func TestRunJobCLIVersionChange(t *testing.T) {
	type previousJob struct {
		id, harness, version string
		status               Status
		finished             int64
		damage               string
	}
	old := previousJob{id: "z-old", harness: "fake", version: "2.0.0", status: StatusSucceeded, finished: 1}
	type testCase struct {
		name    string
		history []previousJob
		current string
		failed  bool
		want    string
	}
	tests := []testCase{
		{name: "first job", current: "2.2.0"},
		{name: "unchanged", history: []previousJob{old}, current: "2.0.0"},
		{name: "changed", history: []previousJob{old}, current: "2.2.0", want: "CLI 版本变化：2.0.0 → 2.2.0"},
		{name: "unknown current", history: []previousJob{old}},
		{name: "run still fails", history: []previousJob{old}, current: "2.2.0", failed: true, want: "CLI 版本变化：2.0.0 → 2.2.0"},
	}
	for _, status := range []Status{StatusSucceeded, StatusFailed, StatusCanceled} {
		latest := previousJob{id: "a-new", harness: "fake", version: "2.1.0", status: status, finished: 2}
		tests = append(tests, testCase{name: "latest " + string(status), history: []previousJob{latest, old}, current: "2.2.0", want: "CLI 版本变化：2.1.0 → 2.2.0"})
	}
	for _, skip := range []string{"empty version", "other harness", "queued", "running", "missing state", "corrupt state", "unreadable state", "missing result", "corrupt result", "unreadable result"} {
		newer := previousJob{id: "a-new", harness: "fake", version: "ignored", status: StatusSucceeded, finished: 3}
		switch skip {
		case "empty version":
			newer.version = ""
		case "other harness":
			newer.harness = "other"
		case "queued", "running":
			newer.status = Status(skip)
		default:
			newer.damage = skip
		}
		tests = append(tests, testCase{name: "skip " + skip, history: []previousJob{old, newer}, current: "2.2.0", want: "CLI 版本变化：2.0.0 → 2.2.0"})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, s, b := workerFixture(t, "write")
			for _, prev := range tc.history {
				st, err := store.Create(prev.id, Request{Harness: prev.harness, Cwd: "/other-repo"})
				if err != nil {
					t.Fatal(err)
				}
				st.Status, st.FinishedAt = prev.status, time.Unix(prev.finished, 0).UTC()
				if err := store.Save(st); err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(Result{JobID: prev.id, Harness: prev.harness, Status: prev.status, CLIVersion: prev.version})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(store.ResultPath(prev.id), data, 0600); err != nil {
					t.Fatal(err)
				}
				if prev.damage != "" {
					kind, file, _ := strings.Cut(prev.damage, " ")
					path := filepath.Join(store.Dir(prev.id), file+".json")
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					switch kind {
					case "corrupt":
						if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
							t.Fatal(err)
						}
					case "unreadable":
						// A directory fails reads even when tests run as root.
						if err := os.Mkdir(path, 0700); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			s.script = func(s *fakeSession) {
				state := cockpit.StateTurnCompleted
				if tc.failed {
					state = cockpit.StateTurnFailed
				}
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: state}
			}
			h := newFakeHarness(b)
			h.Version = func(context.Context) (string, error) { return tc.current, nil }
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(store.ResultPath("j1"))
			if err != nil {
				t.Fatal(err)
			}
			var persisted Result
			if err := json.Unmarshal(data, &persisted); err != nil {
				t.Fatal(err)
			}
			wantStatus := StatusSucceeded
			if tc.failed {
				wantStatus = StatusFailed
			}
			for _, result := range []Result{r, persisted} {
				if result.Status != wantStatus || result.CLIVersion != tc.current {
					t.Fatalf("result=%+v", result)
				}
				if tc.want == "" {
					if contains(result.Warnings, "CLI 版本变化：") {
						t.Fatalf("unexpected version warning: %v", result.Warnings)
					}
				} else if len(result.Warnings) == 0 || result.Warnings[0] != tc.want || !strings.HasPrefix(result.Markdown(), "⚠️ "+tc.want) {
					t.Fatalf("missing first-line warning %q: %s", tc.want, result.Markdown())
				}
			}
			st, err := store.Load("j1")
			if err != nil || st.Status != wantStatus {
				t.Fatalf("state=%+v err=%v", st, err)
			}
		})
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

// Delegated jobs ask the backend to hide their session (codex archives the thread),
// and a failed archive is surfaced as a warning without failing the job.
func TestRunJobHidesSessionAndReportsArchiveFailure(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	s.closeErr = fmt.Errorf("codex: %w th-fake: boom", cockpit.ErrArchive)
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "done"}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !b.gotOpts.Hidden {
		t.Fatal("sub jobs must start hidden sessions")
	}
	if r.Status != StatusSucceeded || !contains(r.Warnings, "归档") {
		t.Fatalf("status=%s warnings=%v", r.Status, r.Warnings)
	}
}

// Ordinary teardown errors (not archive failures) stay out of the summary.
func TestRunJobIgnoresNonArchiveCloseErrors(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	s.closeErr = fmt.Errorf("signal: terminated")
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
	if err != nil || len(r.Warnings) != 0 {
		t.Fatalf("err=%v warnings=%v", err, r.Warnings)
	}
}
