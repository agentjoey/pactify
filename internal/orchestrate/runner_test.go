package orchestrate

import (
	"context"
	"errors"
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
	err := r.Run(context.Background(), "w1", "opencode", "do the work", "/repo")
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
	err := r.Run(context.Background(), "r1", "claude-code", "review task t1", "/work")
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

func TestCmdRunner_GUIKind_Errors(t *testing.T) {
	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, nil)}
	err := r.Run(context.Background(), "g1", "antigravity", "brief", "/repo")
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
	err := r.Run(context.Background(), "x1", "no-such-kind", "brief", "/repo")
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
	err := r.Run(context.Background(), "w1", "opencode", "brief", "/repo")
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
