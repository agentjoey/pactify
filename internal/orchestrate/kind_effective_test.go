package orchestrate

import (
	"context"
	"io"
	"testing"

	"github.com/agentjoey/pactify/internal/roles"
	"github.com/agentjoey/pactify/internal/tokens"
)

// Regression test for kind-effective:
// When a seat is bound to a role profile with a different Kind than lc.Kind,
// all kind-dispatched behavior (command, crossVendorStrip, parseTokenUsage,
// resume/session lifecycle) must follow the RESOLVED effective kind, not lc.Kind.
func TestCmdRunner_KindEffective_ClaudeProfileOverAntigravityLC(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()

	// 1. Role binding: seat "reviewer" is bound to profile with kind: claude-code
	c, err := roles.Load()
	if err != nil {
		t.Fatalf("roles.Load: %v", err)
	}
	if err := c.SetProfile("claude-reviewer", roles.Profile{Kind: "claude-code", Model: "claude-opus-4-8"}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if err := c.Bind("reviewer", "claude-reviewer"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 2. Pre-populate an antigravity session record for this (seat, task)
	if err := RecordSession(dir, "reviewer", "t-eff-1", "antigravity", "stale-agy-conv"); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	// 3. Fake exec emitting claude-shaped JSON output
	var cap runCapture
	claudeOutput := `{"type":"result","usage":{"input_tokens":100,"output_tokens":50}}` + "\n"
	r := CmdRunner{Exec: func(_ context.Context, name string, args []string, d string, env []string, capture io.Writer) error {
		cap.called = true
		cap.name = name
		cap.args = append([]string(nil), args...)
		cap.dir = d
		cap.env = append([]string(nil), env...)
		if capture != nil {
			_, _ = io.WriteString(capture, claudeOutput)
		}
		return nil
	}}

	// 4. LaunchContext specifies lc.Kind = "antigravity" (the caller's misconfigured/roster kind)
	lc := LaunchContext{
		Seat:     "reviewer",
		Kind:     "antigravity",
		Task:     "t-eff-1",
		Project:  "demo",
		Briefing: "review this task",
		RepoDir:  dir,
	}

	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !cap.called {
		t.Fatal("execFn was not called")
	}

	// Command must be claude (from resolved profile)
	if cap.name != "claude" {
		t.Errorf("command = %q, want claude", cap.name)
	}

	// crossVendorStrip must follow claude-code:
	// ANTHROPIC_API_KEY must be KEPT (not blanked), while sibling keys like OPENAI_API_KEY must be BLANKED.
	if hasEnv(cap.env, "ANTHROPIC_API_KEY=") {
		t.Errorf("ANTHROPIC_API_KEY= was emitted (blanked), want claude-code's own key kept in env: %v", cap.env)
	}
	if !hasEnv(cap.env, "OPENAI_API_KEY=") {
		t.Errorf("OPENAI_API_KEY= was missing (not blanked), want sibling vendor key blanked: %v", cap.env)
	}

	// Token usage: must be parsed by claude-code parser (150 tokens) and recorded into tokens.json
	gotTokens := tokens.Load(dir).Get("t-eff-1")
	if gotTokens != 150 {
		t.Errorf("recorded tokens = %d, want 150 (claude-code parser must be used, not antigravity parser)", gotTokens)
	}

	// agy resume must NOT engage: argv must not have --conversation or stale-agy-conv
	if argsHave(cap.args, "--conversation") || argsHave(cap.args, "stale-agy-conv") {
		t.Errorf("args = %v, must NOT trigger antigravity resume flag", cap.args)
	}
}

// Reverse case: seat bound to antigravity profile while lc.Kind is claude-code.
func TestCmdRunner_KindEffective_AntigravityProfileOverClaudeLC(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()

	// 1. Role binding: seat "worker" is bound to profile with kind: antigravity
	c, err := roles.Load()
	if err != nil {
		t.Fatalf("roles.Load: %v", err)
	}
	if err := c.SetProfile("agy-worker", roles.Profile{Kind: "antigravity", Model: "gemini-3.7-flash-medium"}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if err := c.Bind("worker", "agy-worker"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 2. Fake exec emitting agy-shaped JSON output
	var cap runCapture
	agyOutput := `{"conversation_id":"fresh-agy-conv","status":"SUCCESS","response":"done","usage":{"total_tokens":75}}`
	r := CmdRunner{Exec: func(_ context.Context, name string, args []string, d string, env []string, capture io.Writer) error {
		cap.called = true
		cap.name = name
		cap.args = append([]string(nil), args...)
		cap.dir = d
		cap.env = append([]string(nil), env...)
		if capture != nil {
			_, _ = io.WriteString(capture, agyOutput)
		}
		return nil
	}}

	// 3. LaunchContext specifies lc.Kind = "claude-code"
	lc := LaunchContext{
		Seat:     "worker",
		Kind:     "claude-code",
		Task:     "t-eff-2",
		Project:  "demo",
		Briefing: "work this task",
		RepoDir:  dir,
	}

	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !cap.called {
		t.Fatal("execFn was not called")
	}

	// Command must be agy (from resolved profile)
	if cap.name != "agy" {
		t.Errorf("command = %q, want agy", cap.name)
	}

	// crossVendorStrip must follow antigravity: all vendor keys blanked including ANTHROPIC_API_KEY
	if !hasEnv(cap.env, "ANTHROPIC_API_KEY=") {
		t.Errorf("ANTHROPIC_API_KEY= was missing (not blanked), want antigravity strip to blank all vendor keys: %v", cap.env)
	}

	// Token usage: must be parsed by antigravity parser (75 tokens) and recorded
	gotTokens := tokens.Load(dir).Get("t-eff-2")
	if gotTokens != 75 {
		t.Errorf("recorded tokens = %d, want 75 (antigravity parser must be used)", gotTokens)
	}

	// agy session lifecycle: successful run records the conversation id
	if id, ok := LookupSessionKind(dir, "worker", "t-eff-2", "antigravity"); !ok || id != "fresh-agy-conv" {
		t.Errorf("LookupSessionKind = (%q,%v), want (fresh-agy-conv,true)", id, ok)
	}
}

// Unbound seat must resolve kind verbatim and match pre-existing behavior.
func TestCmdRunner_KindEffective_UnboundSeat(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()

	var cap runCapture
	output := `{"usage":{"input_tokens":20,"output_tokens":10}}` + "\n"
	r := CmdRunner{Exec: func(_ context.Context, name string, args []string, d string, env []string, capture io.Writer) error {
		cap.called = true
		cap.name = name
		cap.args = append([]string(nil), args...)
		cap.dir = d
		cap.env = append([]string(nil), env...)
		if capture != nil {
			_, _ = io.WriteString(capture, output)
		}
		return nil
	}}

	lc := LaunchContext{
		Seat:     "unbound-dev",
		Kind:     "claude-code",
		Task:     "t-eff-3",
		Project:  "demo",
		Briefing: "hello",
		RepoDir:  dir,
	}

	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if cap.name != "claude" {
		t.Errorf("command = %q, want claude", cap.name)
	}
	if hasEnv(cap.env, "ANTHROPIC_API_KEY=") {
		t.Errorf("ANTHROPIC_API_KEY must be kept for unbound claude-code seat")
	}
	if got := tokens.Load(dir).Get("t-eff-3"); got != 30 {
		t.Errorf("tokens = %d, want 30", got)
	}
}
