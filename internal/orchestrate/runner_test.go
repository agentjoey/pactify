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
	if len(cap.args) != 2 || cap.args[0] != "run" || cap.args[1] != "do the work" {
		t.Fatalf("args = %v, want [run \"do the work\"]", cap.args)
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
	if len(cap.args) != 2 || cap.args[0] != "-p" || cap.args[1] != "review task t1" {
		t.Fatalf("args = %v, want [-p \"review task t1\"]", cap.args)
	}
	if !hasEnv(cap.env, "PACT_AGENT_ID=r1") {
		t.Fatalf("env missing PACT_AGENT_ID=r1: %v", cap.env)
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
	if NewCmdRunner().Exec == nil {
		t.Fatal("NewCmdRunner().Exec is nil")
	}
}
