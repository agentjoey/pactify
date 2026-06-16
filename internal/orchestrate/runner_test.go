package orchestrate

import (
	"context"
	"errors"
	"reflect"
	"testing"
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
	return func(_ context.Context, name string, args []string, dir string, env []string) error {
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
	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, nil)}
	lc := LaunchContext{Seat: "dev", Kind: "opencode", Task: "t7", Project: "demo", Briefing: "B", RepoDir: "/repo"}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !hasEnv(cap.env, "PACT_AGENT_ID=dev") || !hasEnv(cap.env, "PACT_TASK_ID=t7") || !hasEnv(cap.env, "PACT_PROJECT=demo") {
		t.Fatalf("env missing task/project stamp: %v", cap.env)
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
	want := []string{"run", "--title", "pact:dev", "-m", "deepseek/deepseek-v4-pro", "do the thing"}
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
	want := errors.New("boom")
	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, want)}
	err := r.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "opencode", Briefing: "brief", RepoDir: "/repo"})
	if !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want %v", err, want)
	}
}

// NewCmdRunner wires the production execFn; smoke-check it is non-nil so the
// constructor cannot silently regress to a nil exec (which would panic on Run).
func TestNewCmdRunner_HasExec(t *testing.T) {
	if NewCmdRunner(0).Exec == nil {
		t.Fatal("NewCmdRunner(0).Exec is nil")
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
