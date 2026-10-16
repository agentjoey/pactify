package cockpit

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
)

func TestAgyReviewCompletionExamples(t *testing.T) {
	for _, s := range []string{"I'll report the results below:\n- all 42 tests pass", "Done. All tests pass. I will review the output of CI after you push.", "… We'll check the results in the follow-up PR.", "所有测试通过。如需要，完成后再查看 CI。", "The tests are still running in CI is no longer an issue…"} {
		if agyNeedsCompletion(s) {
			t.Errorf("complete answer matched: %s", s)
		}
	}
	for _, s := range []string{"I started the tests. They are still running; no results yet.", "Tests have been launched in the background. I will check the results later.", "已启动测试，完成后再查看。"} {
		if !agyNeedsCompletion(s) {
			t.Errorf("unfinished answer missed: %s", s)
		}
	}
}
func TestAgyReviewActiveOnlyCurrentTurn(t *testing.T) {
	b, o := agyV2Fixture(t, "always-active")
	s, e := b.Start(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ev := agyTurn(t, s)
	last := ev[len(ev)-1]
	if last.State != StateTurnCompleted {
		t.Fatal(last)
	}
	for _, e := range ev {
		if e.Final && !strings.Contains(e.Text, "round=2") {
			t.Fatal(e.Text)
		}
	}
}
func TestAgyReviewMismatchSticky(t *testing.T) {
	b, o := agyV2Fixture(t, "mismatch")
	s, e := b.Start(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ev := agyTurn(t, s)
	last := ev[len(ev)-1]
	if last.State != StateTurnFailed || !strings.Contains(last.Err, "model_mismatch") || ClassifyFailure("agy", last.Err, nil, time.Now()).Class == "transient" {
		t.Fatal(last)
	}
	if e := s.Prompt(context.Background(), UserMessage{Text: "continue"}); e == nil || !strings.Contains(e.Error(), "model_mismatch") {
		t.Fatalf("continued wrong model: %v", e)
	}
}
func TestAgyReviewBlockedStdinCancellation(t *testing.T) {
	b, o := agyV2Fixture(t, "no-read")
	s, e := b.Start(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sent := make(chan error, 1)
	go func() { sent <- s.Prompt(ctx, UserMessage{Text: strings.Repeat("x", 512*1024)}) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	for _, f := range []func() error{func() error { return s.Interrupt(context.Background()) }, s.Close} {
		done := make(chan error, 1)
		go func() { done <- f() }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("cancellation blocked on stdin mutex")
			_ = s.(*agySession).process.cmd.Process.Kill()
			return
		}
	}
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("Prompt remained blocked")
	}
}
func TestAgyReviewDiagnosticsReset(t *testing.T) {
	b, o := agyV2Fixture(t, "denied-once")
	s, e := b.Start(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	agyTurn(t, s)
	ev := agyTurn(t, s)
	if ev[len(ev)-1].State != StateTurnCompleted {
		t.Fatal(ev[len(ev)-1])
	}
}
func TestAgyReviewResumeRequiresOptions(t *testing.T) {
	b, _ := agyV2Fixture(t, "")
	s, e := b.Resume(context.Background(), "id")
	if s != nil {
		_ = s.Close()
	}
	if e == nil {
		t.Fatal("optionless resume could use user HOME")
	}
}
func TestAgyReviewWorkspaceHookDirectories(t *testing.T) {
	for _, dir := range []string{".agents", ".agent", "_agents"} {
		t.Run(dir, func(t *testing.T) {
			b, o := agyV2Fixture(t, "")
			o.Access = AccessReadOnly
			o.MinimalConfig = true
			_ = os.Mkdir(filepath.Join(o.RepoDir, dir), 0700)
			_ = os.WriteFile(filepath.Join(o.RepoDir, dir, "hooks.json"), []byte("{}"), 0600)
			s, e := b.Start(context.Background(), o)
			if s != nil {
				_ = s.Close()
			}
			if e == nil || !strings.Contains(e.Error(), "workspace hooks") {
				t.Fatal("unsafe workspace accepted", e)
			}
		})
	}
}
func TestAgyReviewPolicyDefaultDenyAndNativeHook(t *testing.T) {
	home := t.TempDir()
	_, e := AgyEnv(StartOpts{MinimalConfig: true, Access: AccessReadOnly, Env: []string{"HOME=" + home}})
	if e != nil {
		t.Fatal(e)
	}
	hooks, _ := os.ReadFile(filepath.Join(home, ".gemini/config/hooks.json"))
	if strings.Contains(string(hooks), "python3") || !strings.Contains(string(hooks), "agy-hook") {
		t.Fatal("policy does not use pactify binary")
	}
}
func TestAgyReviewQuotaSkipsBadBuckets(t *testing.T) {
	b := &agyBackend{command: func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", "%s", `startup notice
{"status":"SUCCESS","num_turns":0,"command":{"name":"usage","data":{"groups":[{"name":"Gemini Models","buckets":[{"id":"bad","remaining_fraction":0.7,"reset_time":"odd"},{"id":"good","remaining_fraction":0.9,"reset_time":"2099-01-01T00:00:00Z"}]}]}}}`)
	}}
	q, e := b.Quota(context.Background(), StartOpts{})
	if e != nil || len(q.Buckets) != 1 || q.Buckets[0].ID != "good" {
		t.Fatalf("%+v %v", q, e)
	}
}
func TestAgyReviewNoCumulativeResumeFallback(t *testing.T) {
	b, o := agyV2Fixture(t, "result-only")
	s, e := b.(OptsResumer).ResumeWith(context.Background(), "old", o)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var usage Usage
	partial := false
	for _, e := range agyTurn(t, s) {
		if e.Usage != nil {
			usage = *e.Usage
		}
		if e.State == "usage_partial" {
			partial = true
		}
	}
	if usage.TotalTokens != 0 || !partial {
		t.Fatalf("historical result counted: %+v partial=%t", usage, partial)
	}
}

func TestAgyReviewHookFailsClosed(t *testing.T) {
	payload := `{"toolCall":{"name":"write_to_file","args":{"TargetFile":"/outside/x"}}}`
	for _, mode := range []string{"", "1", "unexpected", "0"} {
		var out strings.Builder
		RunAgyHook(context.Background(), strings.NewReader(payload), &out, mode)
		var decision struct {
			Decision string `json:"decision"`
		}
		_ = json.Unmarshal([]byte(out.String()), &decision)
		if (decision.Decision == "allow") != (mode == "0") {
			t.Errorf("mode=%q out=%s", mode, out.String())
		}
	}
	for _, payload := range []string{"bad", `{"toolCall":{"name":"run_command","args":{"CommandLine":"git diff --output=/outside/x"}}}`, `{"toolCall":{"name":"run_command","args":{"CommandLine":"ls > /outside/x"}}}`} {
		var out strings.Builder
		RunAgyHook(context.Background(), strings.NewReader(payload), &out, "")
		if !strings.Contains(out.String(), `"decision":"deny"`) {
			t.Fatal(out.String())
		}
	}
}
func TestAgyReviewHookTimeoutDenies(t *testing.T) {
	in, out := io.Pipe()
	defer in.Close()
	defer out.Close()
	var answer strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	RunAgyHook(ctx, in, &answer, "0")
	if !strings.Contains(answer.String(), `"decision":"deny"`) {
		t.Fatal(answer.String())
	}
}

func TestAgyReviewModelHintIsPinned(t *testing.T) {
	models := make([]string, 12)
	for i := range models {
		models[i] = "gemini-3.8-flash-low"
	}
	if hint := modelChoices("agy", "bad", models); !strings.Contains(hint, "agy models --model gemini-3.8-flash-low") {
		t.Fatal(hint)
	}
}

func TestAgyReviewWorkspaceHooksThroughSymlink(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	sub := filepath.Join(repo, "sub")
	for _, dir := range []string{sub, filepath.Join(repo, ".git"), filepath.Join(repo, ".agent")} {
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	writeAgyHooks(t, filepath.Join(repo, ".agent"))
	alias := filepath.Join(t.TempDir(), "alias")
	if e := os.Symlink(sub, alias); e != nil {
		t.Fatal(e)
	}
	_, e := AgyEnv(StartOpts{RepoDir: alias, Access: AccessReadOnly, MinimalConfig: true, Env: []string{"HOME=" + t.TempDir()}})
	if e == nil || !strings.Contains(e.Error(), filepath.Join(repo, ".agent")) {
		t.Fatalf("real workspace hooks missed: %v", e)
	}
}

func TestAgyReviewCatalogErrorsPinModel(t *testing.T) {
	b := &agyBackend{command: func(ctx context.Context, args ...string) *exec.Cmd { return exec.CommandContext(ctx, "false") }}
	_, e := b.models(context.Background(), StartOpts{})
	if e == nil || !strings.Contains(e.Error(), "agy models --model "+AgyDefaultModel) {
		t.Fatalf("unqualified command in error: %v", e)
	}
}

func TestAgyReviewWorkspaceRefusedBeforeDiscovery(t *testing.T) {
	repo := t.TempDir()
	for _, name := range []string{".git", ".agent"} {
		if e := os.Mkdir(filepath.Join(repo, name), 0700); e != nil {
			t.Fatal(e)
		}
	}
	writeAgyHooks(t, filepath.Join(repo, ".agent"))
	called := false
	b := &agyBackend{command: func(ctx context.Context, args ...string) *exec.Cmd {
		called = true
		return exec.CommandContext(ctx, "printf", "%s", "gemini-3.8-flash-low\tFlash\n")
	}}
	_, e := b.Start(context.Background(), StartOpts{RepoDir: repo, Model: AgyDefaultModel, Access: AccessReadOnly, MinimalConfig: true, Env: []string{"HOME=" + t.TempDir()}})
	if e == nil || called {
		t.Fatalf("workspace hooks reached agy discovery: called=%t err=%v", called, e)
	}
}

func TestAgyReviewWorkspaceDefaultCurrentDir(t *testing.T) {
	repo := t.TempDir()
	sub := filepath.Join(repo, "sub")
	for _, dir := range []string{sub, filepath.Join(repo, ".git"), filepath.Join(repo, ".agent")} {
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	writeAgyHooks(t, filepath.Join(repo, ".agent"))
	t.Chdir(sub)
	if e := AgyReadOnlyWorkspace(""); e == nil {
		t.Fatal("default cwd hid parent workspace hooks")
	}
}

func TestAgyReviewQuotaSkipsBadBucketTypes(t *testing.T) {
	for _, bad := range []string{`{"id":"bad","remaining_fraction":"broken","reset_time":"2099-01-01T00:00:00Z"}`, `{"id":"bad","remaining_fraction":0.7,"reset_time":123}`} {
		b := &agyBackend{command: func(ctx context.Context, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "printf", "%s", `{"status":"SUCCESS","num_turns":0,"command":{"name":"usage","data":{"groups":[{"name":"Gemini Models","buckets":[`+bad+`,{"id":"good","remaining_fraction":0.9,"reset_time":"2099-01-01T00:00:00Z"}]}]}}}`)
		}}
		q, e := b.Quota(context.Background(), StartOpts{})
		if e != nil || len(q.Buckets) != 1 || q.Buckets[0].ID != "good" {
			t.Errorf("valid bucket lost: %+v err=%v", q, e)
		}
	}
}

// Simulate a Close that returns only once the peer has consumed EOF. Descendant
// ownership must already be captured by then, regardless of scheduler timing.
type agyEOFBarrier struct {
	io.WriteCloser
	gone func() bool
}

func (w agyEOFBarrier) Close() error {
	e := w.WriteCloser.Close()
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) && !w.gone() {
		time.Sleep(time.Millisecond)
	}
	return e
}
func TestAgyReviewCancellationSnapshotsBeforeEOF(t *testing.T) {
	b, o := agyFixture(t, "eof-orphan", AccessWrite)
	file := filepath.Join(t.TempDir(), "pid")
	o.Env = append(o.Env, "AGY_EOF_CHILD="+file, "GORACE=atexit_sleep_ms=0")
	s, e := b.Start(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Prompt(context.Background(), UserMessage{Text: "SECRET-PROMPT"}); e != nil {
		t.Fatal(e)
	}
	select {
	case <-s.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("no init")
	}
	var data []byte
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		data, _ = os.ReadFile(file)
		if len(data) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	pid, e := strconv.Atoi(string(data))
	if e != nil {
		t.Fatal(e)
	}
	child, e := procx.Inspect(pid)
	if e != nil {
		t.Fatal(e)
	}
	defer child.Signal(syscall.SIGKILL, false)
	p := s.(*agySession).process
	p.stdin = agyEOFBarrier{WriteCloser: p.stdin, gone: func() bool { alive, _ := p.identity.Alive(); return !alive }}
	_ = s.Interrupt(context.Background())
	if alive, e := child.Alive(); e != nil || alive {
		t.Fatalf("EOF orphan escaped cancellation: alive=%t err=%v", alive, e)
	}
}

func writeAgyHooks(t *testing.T, dir string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte("{}"), 0600); e != nil {
		t.Fatal(e)
	}
}

// A .agent folder used for notes (no hooks.json) must not block read-only.
func TestAgyReadOnlyWorkspaceAllowsFoldersWithoutHooks(t *testing.T) {
	repo := t.TempDir()
	for _, dir := range []string{".git", ".agent", ".agents", "_agents"} {
		if e := os.Mkdir(filepath.Join(repo, dir), 0700); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(repo, ".agent", "CURRENT.md"), []byte("notes"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := AgyReadOnlyWorkspace(repo); e != nil {
		t.Fatalf("folder without hooks.json refused: %v", e)
	}
	writeAgyHooks(t, filepath.Join(repo, "_agents"))
	if e := AgyReadOnlyWorkspace(repo); e == nil || !strings.Contains(e.Error(), "hooks.json") {
		t.Fatalf("hooks.json not refused: %v", e)
	}
}
