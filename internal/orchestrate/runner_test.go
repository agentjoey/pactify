package orchestrate

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/tokens"
)

// runCapture records the arguments a fake execFn was invoked with, so tests can
// assert on the resolved command line, working dir and injected env.
type runCapture struct {
	called bool
	name   string
	args   []string
	dir    string
	env    []string
}

// fakeRunExec returns an execFn that records its inputs into cap and returns ret.
func fakeRunExec(cap *runCapture, ret error) execFn {
	return func(_ context.Context, name string, args []string, dir string, env []string, _ io.Writer) error {
		cap.called = true
		cap.name = name
		cap.args = args
		cap.dir = dir
		cap.env = env
		return ret
	}
}

func hasEnv(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

func TestCmdRunner_Opencode(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, nil)}
	err := r.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "opencode", Briefing: "do the work", RepoDir: "/repo"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !cap.called {
		t.Fatal("execFn was not called")
	}
	if cap.name != "opencode" {
		t.Fatalf("name = %q, want opencode", cap.name)
	}
	// The exact flag list (model pin etc.) is agent.go's contract, tested in
	// agent_test. Here we only assert the command resolves, "run" leads, the
	// briefing is substituted into an arg, and no literal {briefing} survives.
	if cap.args[0] != "run" || !argsHave(cap.args, "do the work") || argsHave(cap.args, "{briefing}") {
		t.Fatalf("args = %v, want run + substituted briefing", cap.args)
	}
	if cap.dir != "/repo" {
		t.Fatalf("dir = %q, want /repo", cap.dir)
	}
	if !hasEnv(cap.env, "PACT_AGENT_ID=w1") {
		t.Fatalf("env missing PACT_AGENT_ID=w1: %v", cap.env)
	}
}

func TestCmdRunner_ClaudeCode(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, nil)}
	err := r.Run(context.Background(), LaunchContext{Seat: "r1", Kind: "claude-code", Briefing: "review task t1", RepoDir: "/work"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cap.name != "claude" {
		t.Fatalf("name = %q, want claude", cap.name)
	}
	if cap.args[0] != "-p" || !argsHave(cap.args, "review task t1") || argsHave(cap.args, "{briefing}") {
		t.Fatalf("args = %v, want -p + substituted briefing", cap.args)
	}
	if !hasEnv(cap.env, "PACT_AGENT_ID=r1") {
		t.Fatalf("env missing PACT_AGENT_ID=r1: %v", cap.env)
	}
}

func TestRunnerStampsTaskAndProjectEnv(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, nil)}
	lc := LaunchContext{Seat: "dev", Kind: "opencode", Task: "t7", Project: "demo", Briefing: "B", RepoDir: "/repo"}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !hasEnv(cap.env, "PACT_AGENT_ID=dev") || !hasEnv(cap.env, "PACT_TASK_ID=t7") || !hasEnv(cap.env, "PACT_PROJECT=demo") {
		t.Fatalf("env missing task/project stamp: %v", cap.env)
	}
	// PACT_DIR pins the worker's pact to the driver's worktree (absolute) so its
	// checkpoint can't land in a divergent ledger.
	if !hasEnv(cap.env, "PACT_DIR=/repo/.pact") {
		t.Fatalf("env missing PACT_DIR pin to the worktree: %v", cap.env)
	}
}

func TestGeminiEnv(t *testing.T) {
	orig := geminiKey
	t.Cleanup(func() { geminiKey = orig })

	// non-gemini command → never injects, key fn not consulted.
	geminiKey = func() (string, error) { t.Fatal("geminiKey should not be called"); return "", nil }
	if env := geminiEnv("claude"); env != nil {
		t.Fatalf("non-gemini: env=%v, want nil", env)
	}

	// gemini seat with a Keychain key → GEMINI_API_KEY injected (trimmed).
	geminiKey = func() (string, error) { return "  AIza-secret  ", nil }
	if env := geminiEnv("gemini"); !hasEnv(env, "GEMINI_API_KEY=AIza-secret") {
		t.Fatalf("gemini env = %v, want GEMINI_API_KEY", env)
	}

	// gemini seat with NO key → no-op (keeps existing auth), never errors.
	geminiKey = func() (string, error) { return "", errors.New("not in Keychain") }
	if env := geminiEnv("gemini"); env != nil {
		t.Fatalf("gemini without key should be a no-op, got %v", env)
	}
}

func TestGLMEnv(t *testing.T) {
	origTok, origURL := glmToken, glmBaseURL
	t.Cleanup(func() { glmToken, glmBaseURL = origTok, origURL })
	// Pin the endpoint to the global default for the base cases (so the test
	// never reads the host's real Keychain override).
	glmBaseURL = func() string { return glmDefaultBaseURL }

	// non-GLM command/model → no GLM env, token fn never consulted.
	glmToken = func() (string, error) { t.Fatal("glmToken should not be called"); return "", nil }
	if env, err := glmEnv("claude", "claude-opus-4-8"); err != nil || env != nil {
		t.Fatalf("non-glm claude: env=%v err=%v, want nil,nil", env, err)
	}
	if env, err := glmEnv("opencode", "glm-4.7"); err != nil || env != nil {
		t.Fatalf("glm on non-claude: env=%v err=%v, want nil,nil", env, err)
	}

	// GLM seat → base URL + token injected.
	glmToken = func() (string, error) { return "zai-secret", nil }
	env, err := glmEnv("claude", "glm-4.7")
	if err != nil {
		t.Fatalf("glm claude: %v", err)
	}
	if !hasEnv(env, "ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic") || !hasEnv(env, "ANTHROPIC_AUTH_TOKEN=zai-secret") {
		t.Fatalf("glm env = %v, want base URL + token", env)
	}

	// Keychain endpoint override (china coding plan) flows into the env.
	glmBaseURL = func() string { return "https://open.bigmodel.cn/api/anthropic" }
	env, err = glmEnv("claude", "glm-4.6")
	if err != nil {
		t.Fatalf("glm claude (china): %v", err)
	}
	if !hasEnv(env, "ANTHROPIC_BASE_URL=https://open.bigmodel.cn/api/anthropic") {
		t.Fatalf("china glm env = %v, want open.bigmodel.cn base URL", env)
	}

	// GLM seat but missing token → actionable error.
	glmToken = func() (string, error) { return "", errors.New("not in Keychain") }
	if _, err := glmEnv("claude", "glm-4.6"); err == nil {
		t.Fatal("expected error when GLM token missing")
	}
}

func TestTagOpencodeSession(t *testing.T) {
	// opencode run args get a per-seat --title inserted right after "run".
	got := tagOpencodeSession("opencode", "dev", []string{"run", "-m", "deepseek/deepseek-v4-pro", "do the thing"})
	want := []string{"run", "--title", "[pact:dev]", "-m", "deepseek/deepseek-v4-pro", "do the thing"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("opencode tag = %v, want %v", got, want)
	}
	// Non-opencode commands are untouched.
	claude := []string{"-p", "--model", "claude-opus-4-8", "brief"}
	if got := tagOpencodeSession("claude", "dev", claude); !reflect.DeepEqual(got, claude) {
		t.Errorf("claude args mutated: %v", got)
	}
	// Defensive: opencode args not starting with "run" are left alone.
	odd := []string{"models"}
	if got := tagOpencodeSession("opencode", "dev", odd); !reflect.DeepEqual(got, odd) {
		t.Errorf("non-run opencode args mutated: %v", got)
	}
}

func TestCmdRunner_GUIKind_Errors(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, nil)}
	err := r.Run(context.Background(), LaunchContext{Seat: "g1", Kind: "antigravity", Briefing: "brief", RepoDir: "/repo"})
	if err == nil {
		t.Fatal("expected error for GUI kind antigravity, got nil")
	}
	if cap.called {
		t.Fatal("execFn must not be called for a kind without a headless runner")
	}
}

func TestCmdRunner_UnknownKind_Errors(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, nil)}
	err := r.Run(context.Background(), LaunchContext{Seat: "x1", Kind: "no-such-kind", Briefing: "brief", RepoDir: "/repo"})
	if err == nil {
		t.Fatal("expected error for unknown kind, got nil")
	}
	if cap.called {
		t.Fatal("execFn must not be called for an unknown kind")
	}
}

func TestCmdRunner_ExecError_Propagates(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	want := errors.New("boom")
	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, want)}
	err := r.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "opencode", Briefing: "brief", RepoDir: "/repo"})
	if !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want %v", err, want)
	}
}

// emitExec returns an execFn that writes out to the capture sink (simulating an
// agent's headless stdout) before returning ret — so token-recording can be tested
// without spawning a real process.
func emitExec(out string, ret error) execFn {
	return func(_ context.Context, _ string, _ []string, _ string, _ []string, capture io.Writer) error {
		if capture != nil {
			_, _ = io.WriteString(capture, out)
		}
		return ret
	}
}

// A run whose stdout carries usage JSON records the parsed total into the repo's
// token store, keyed by task — the write side the dashboard's cost lens reads.
func TestCmdRunner_RecordsTokens(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	r := CmdRunner{Exec: emitExec(`{"usage":{"input_tokens":120,"output_tokens":80}}`+"\n", nil)}
	err := r.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "claude-code", Task: "t-x", Briefing: "go", RepoDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := tokens.Load(dir).Get("t-x"); got != 200 {
		t.Fatalf("recorded tokens for t-x = %d, want 200", got)
	}
}

// Two stints on the same task accumulate (Add sums); a failing run still records
// the usage it emitted before failing (telemetry is independent of the verdict).
func TestCmdRunner_RecordsTokens_AccumulatesAcrossRuns(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	lc := LaunchContext{Seat: "w1", Kind: "claude-code", Task: "t-y", Briefing: "go", RepoDir: dir}
	r1 := CmdRunner{Exec: emitExec(`{"usage":{"input_tokens":50,"output_tokens":50}}`, nil)}
	if err := r1.Run(context.Background(), lc); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	r2 := CmdRunner{Exec: emitExec(`{"usage":{"input_tokens":30,"output_tokens":40}}`, errors.New("rework"))}
	_ = r2.Run(context.Background(), lc)
	if got := tokens.Load(dir).Get("t-y"); got != 170 {
		t.Fatalf("accumulated tokens for t-y = %d, want 170", got)
	}
	if runs := tokens.Load(dir).Tasks["t-y"].Runs; runs != 2 {
		t.Fatalf("runs for t-y = %d, want 2", runs)
	}
}

// No task id (a non-orchestrated launch) records nothing and never panics; output
// with no usage block is likewise a silent no-op.
func TestCmdRunner_RecordsTokens_NoopWithoutTaskOrUsage(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	noTask := CmdRunner{Exec: emitExec(`{"usage":{"input_tokens":9,"output_tokens":9}}`, nil)}
	if err := noTask.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "claude-code", Briefing: "go", RepoDir: dir}); err != nil {
		t.Fatalf("run (no task): %v", err)
	}
	noUsage := CmdRunner{Exec: emitExec("just some chatter, no json\n", nil)}
	if err := noUsage.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "claude-code", Task: "t-z", Briefing: "go", RepoDir: dir}); err != nil {
		t.Fatalf("run (no usage): %v", err)
	}
	if got := tokens.Load(dir).Get("t-z"); got != 0 {
		t.Fatalf("tokens for t-z = %d, want 0 (no usage in output)", got)
	}
	if len(tokens.Load(dir).Tasks) != 0 {
		t.Fatalf("token store should be empty, got %d entries", len(tokens.Load(dir).Tasks))
	}
}

// tailWriter must bound memory yet preserve the trailing usage line a chatty
// stream-json run emits, so token parsing still finds it after truncation.
func TestTailWriter_KeepsTailUnderCap(t *testing.T) {
	w := &tailWriter{max: 64}
	for i := 0; i < 100; i++ {
		_, _ = io.WriteString(w, "noise noise noise\n")
	}
	_, _ = io.WriteString(w, `{"usage":{"input_tokens":7,"output_tokens":3}}`+"\n")
	if len(w.buf) > 64 {
		t.Fatalf("tail buffer = %d bytes, want <= 64 (cap not enforced)", len(w.buf))
	}
	if n, ok := tokens.Parse("claude-code", w.String()); !ok || n != 10 {
		t.Fatalf("Parse(tail) = (%d,%v), want (10,true) — trailing usage line lost", n, ok)
	}
}

// NewCmdRunner wires the production execFn; smoke-check it is non-nil so the
// constructor cannot silently regress to a nil exec (which would panic on Run).
func TestNewCmdRunner_HasExec(t *testing.T) {
	if NewCmdRunner(0).Exec == nil {
		t.Fatal("NewCmdRunner(0).Exec is nil")
	}
}

func TestRunMirrorsStdoutToStreamFile(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	r := CmdRunner{Exec: func(ctx context.Context, name string, args []string, d string, env []string, capture io.Writer) error {
		capture.Write([]byte("hello from agent\n"))
		return nil
	}}
	err := r.Run(context.Background(), LaunchContext{Kind: "opencode", Seat: "opencode", Task: "t1", Project: "p", RepoDir: dir, Briefing: "b"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(StreamPath(dir, "t1"))
	if string(got) != "hello from agent\n" {
		t.Fatalf("stream file = %q", got)
	}
}

// argsHave reports whether args contains an element equal to want.
func argsHave(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestRunnerSubstitutesSeatToken(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	rp := agent.RunnerProfile{Command: "myagent", DefaultModel: "m1",
		BuildArgs: func(model string, _ agent.PermPosture, briefing string) []string {
			return []string{"run", "--id", "{seat}", briefing}
		}}
	if err := agent.RegisterExternal(agent.External{Kind: "seatkind", Binary: "myagent", Runner: &rp}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { agent.UnregisterExternal("seatkind") })

	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, nil)}
	if err := r.Run(context.Background(), LaunchContext{Seat: "dev", Kind: "seatkind", Briefing: "B", RepoDir: "/r"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if argsHave(cap.args, "{seat}") || !argsHave(cap.args, "dev") {
		t.Fatalf("args = %v, want {seat}->dev", cap.args)
	}
}

func TestParseCodexThreadID(t *testing.T) {
	if got := parseCodexThreadID(`{"type":"thread.started","thread_id":"abc-123"}`); got != "abc-123" {
		t.Errorf("single line: got %q, want abc-123", got)
	}
	out := "chatter before\n" + `{"type":"thread.started","thread_id":"xyz-9"}` + "\n{\"usage\":{}}\n"
	if got := parseCodexThreadID(out); got != "xyz-9" {
		t.Errorf("multi-line: got %q, want xyz-9", got)
	}
	if got := parseCodexThreadID("no thread id here"); got != "" {
		t.Errorf("no match: got %q, want empty", got)
	}
}

func TestCodexResumeArgs(t *testing.T) {
	base := []string{"exec", "--json", "--sandbox", "danger-full-access", "-m", "o4", "{briefing}"}
	got := codexResumeArgs(base, "th-1")
	want := []string{"exec", "resume", "th-1", "--json", "-m", "o4", "{briefing}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resume args = %v, want %v", got, want)
	}
	if argsHave(got, "--sandbox") {
		t.Error("resume args must not contain --sandbox")
	}

	// No model, no sandbox: still inserts resume and id.
	base2 := []string{"exec", "--json", "{briefing}"}
	got2 := codexResumeArgs(base2, "th-2")
	want2 := []string{"exec", "resume", "th-2", "--json", "{briefing}"}
	if !reflect.DeepEqual(got2, want2) {
		t.Errorf("minimal resume args = %v, want %v", got2, want2)
	}
}

func TestCmdRunner_Codex_RecordsThreadID(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	r := CmdRunner{Exec: emitExec(`{"type":"thread.started","thread_id":"th-abc"}`+"\n", nil)}
	lc := LaunchContext{Seat: "w1", Kind: "codex-cli", Task: "t1", Briefing: "do it", RepoDir: dir}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, ok := LookupSession(dir, "w1", "t1")
	if !ok || got != "th-abc" {
		t.Fatalf("LookupSession = %q ok=%v, want th-abc true", got, ok)
	}
}

func TestCmdRunner_Codex_ResumeRetry(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	steps := []struct {
		out string
		ret error
	}{
		{out: `{"type":"thread.started","thread_id":"th-abc"}` + "\n", ret: nil},
		{out: "", ret: errors.New("resume failed")},
		{out: `{"type":"thread.started","thread_id":"th-new"}` + "\n", ret: nil},
	}
	var calls []struct {
		name string
		args []string
	}
	fn := func(_ context.Context, name string, args []string, _ string, _ []string, capture io.Writer) error {
		calls = append(calls, struct {
			name string
			args []string
		}{name, append([]string(nil), args...)})
		idx := len(calls) - 1
		if idx < len(steps) && capture != nil {
			_, _ = io.WriteString(capture, steps[idx].out)
		}
		if idx < len(steps) {
			return steps[idx].ret
		}
		return nil
	}

	r := CmdRunner{Exec: fn}
	lc := LaunchContext{Seat: "w1", Kind: "codex-cli", Task: "t1", Briefing: "do it", RepoDir: dir}

	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if err := r.Run(context.Background(), lc); err == nil {
		t.Fatal("run 2 expected resume error, got nil")
	}
	recs, _ := LoadSessions(dir)
	t.Logf("after run 2: %+v", recs)
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("run 3: %v", err)
	}

	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3", len(calls))
	}

	// Run 1 is a cold start.
	if calls[0].args[0] != "exec" || argsHave(calls[0].args, "resume") {
		t.Fatalf("run 1 args = %v, want cold exec", calls[0].args)
	}

	// Run 2 resumes the recorded thread id and omits --sandbox.
	if !argsHave(calls[1].args, "resume") || !argsHave(calls[1].args, "th-abc") {
		t.Fatalf("run 2 args = %v, want resume th-abc", calls[1].args)
	}
	if argsHave(calls[1].args, "--sandbox") {
		t.Fatalf("run 2 args must not contain --sandbox: %v", calls[1].args)
	}

	// After the failed resume the record is gone, so run 3 is cold again.
	if calls[2].args[0] != "exec" || argsHave(calls[2].args, "resume") {
		t.Fatalf("run 3 args = %v, want cold exec after self-heal", calls[2].args)
	}

	// Run 3 succeeded, so a FRESH session record exists (th-new) — the stale
	// th-abc was removed by the failed-resume self-heal, not left behind.
	id, ok := LookupSession(dir, "w1", "t1")
	if !ok || id != "th-new" {
		t.Fatalf("after run 3 want fresh session th-new, got %q ok=%v", id, ok)
	}
}

// The tier-derived effort budget (LaunchContext.Effort) reaches the argv of a
// kind that declares effort control: claude-code gets --effort appended.
func TestCmdRunner_EffortFlagAppended(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, nil)}
	lc := LaunchContext{Seat: "w1", Kind: "claude-code", Briefing: "do it", RepoDir: "/repo", Effort: "low"}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"-p", "--no-session-persistence", "--dangerously-skip-permissions", "--model", "claude-opus-4-8", "do it", "--effort", "low"}
	if !reflect.DeepEqual(cap.args, want) {
		t.Fatalf("args = %v, want %v", cap.args, want)
	}
}

// Empty Effort must leave the argv byte-for-byte unchanged, and a kind with no
// effort control (opencode) must ignore a set Effort — its launch stays
// byte-for-byte identical either way.
func TestCmdRunner_EffortZeroChange(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())

	// Capable kind, empty effort → default argv, no effort flag.
	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, nil)}
	if err := r.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "claude-code", Briefing: "do it", RepoDir: "/repo"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"-p", "--no-session-persistence", "--dangerously-skip-permissions", "--model", "claude-opus-4-8", "do it"}
	if !reflect.DeepEqual(cap.args, want) {
		t.Fatalf("empty-effort args = %v, want %v", cap.args, want)
	}

	// Incapable kind, effort set → argv identical to an effort-less launch.
	var withEffort, withoutEffort runCapture
	rw := CmdRunner{Exec: fakeRunExec(&withEffort, nil)}
	ro := CmdRunner{Exec: fakeRunExec(&withoutEffort, nil)}
	lc := LaunchContext{Seat: "w1", Kind: "opencode", Briefing: "do it", RepoDir: "/repo"}
	lcEff := lc
	lcEff.Effort = "high"
	if err := rw.Run(context.Background(), lcEff); err != nil {
		t.Fatalf("Run with effort: %v", err)
	}
	if err := ro.Run(context.Background(), lc); err != nil {
		t.Fatalf("Run without effort: %v", err)
	}
	if !reflect.DeepEqual(withEffort.args, withoutEffort.args) {
		t.Fatalf("opencode args changed by effort: with=%v without=%v", withEffort.args, withoutEffort.args)
	}
}
