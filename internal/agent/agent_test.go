package agent

import (
	"strings"
	"testing"
)

func TestKindsSortedAndComplete(t *testing.T) {
	got := strings.Join(Kinds(), ",")
	want := "antigravity,claude-code,claude-desktop,codex-app,codex-cli,cursor-cli,gemini-cli,kimi-cli,opencode"
	if got != want {
		t.Fatalf("Kinds() = %q, want %q", got, want)
	}
}

func TestOpencodeAdapter(t *testing.T) {
	a, ok := Get("opencode")
	if !ok {
		t.Fatal("opencode not registered")
	}
	if a.DefaultEntry() != "AGENTS.md" {
		t.Fatalf("entry = %q", a.DefaultEntry())
	}
	c := a.Config()
	if c.Path != "opencode.json" || c.Scope != Project || c.Format != JSONOpencode {
		t.Fatalf("config = %+v", c)
	}
	inv := a.Invocation("opencode", "/repo")
	if inv.Command != "pactify" || strings.Join(inv.Args, " ") != "mcp" || inv.Env["PACT_AGENT_ID"] != "opencode" {
		t.Fatalf("invocation = %+v", inv)
	}
}

func TestDesktopAdapterAddsProject(t *testing.T) {
	a, _ := Get("antigravity")
	if a.DefaultEntry() != "" {
		t.Fatalf("desktop app should have no entry file, got %q", a.DefaultEntry())
	}
	if a.Config().Scope != Global || a.Config().Format != JSONMcpServers {
		t.Fatalf("config = %+v", a.Config())
	}
	inv := a.Invocation("antigravity", "/abs/repo")
	if strings.Join(inv.Args, " ") != "mcp --project /abs/repo" {
		t.Fatalf("desktop args = %v", inv.Args)
	}
}

func TestExpandPathHome(t *testing.T) {
	got := ExpandPath("~/.codex/config.toml")
	if strings.HasPrefix(got, "~") || !strings.HasSuffix(got, "/.codex/config.toml") {
		t.Fatalf("ExpandPath did not expand ~: %q", got)
	}
	if ExpandPath("opencode.json") != "opencode.json" {
		t.Fatal("relative path must pass through unchanged")
	}
}

func TestExpandPathNamedHomePassesThrough(t *testing.T) {
	if got := ExpandPath("~user/x"); got != "~user/x" {
		t.Fatalf("named-home form must pass through unchanged, got %q", got)
	}
}

func TestUnknownKind(t *testing.T) {
	if _, ok := Get("nope"); ok {
		t.Fatal("unknown kind should not resolve")
	}
}

func TestKimiCliAdapter(t *testing.T) {
	a, ok := Get("kimi-cli")
	if !ok {
		t.Fatal("kimi-cli not registered")
	}
	if a.DefaultEntry() != "AGENTS.md" {
		t.Fatalf("entry = %q, want AGENTS.md", a.DefaultEntry())
	}
	c := a.Config()
	if c.Path != "~/.kimi-code/mcp.json" || c.Scope != Global || c.Format != JSONMcpServers {
		t.Fatalf("config = %+v", c)
	}
	inv := a.Invocation("kimi-cli", "/repo")
	if inv.Command != "pactify" || strings.Join(inv.Args, " ") != "mcp" || inv.Env["PACT_AGENT_ID"] != "kimi-cli" {
		t.Fatalf("invocation = %+v", inv)
	}
}

func TestCursorCliAdapter(t *testing.T) {
	a, ok := Get("cursor-cli")
	if !ok {
		t.Fatal("cursor-cli not registered")
	}
	if a.DefaultEntry() != "AGENTS.md" {
		t.Fatalf("entry = %q, want AGENTS.md", a.DefaultEntry())
	}
	c := a.Config()
	if c.Path != ".cursor/mcp.json" || c.Scope != Project || c.Format != JSONMcpServers {
		t.Fatalf("config = %+v", c)
	}
	inv := a.Invocation("cursor-cli", "/repo")
	if inv.Command != "pactify" || strings.Join(inv.Args, " ") != "mcp" || inv.Env["PACT_AGENT_ID"] != "cursor-cli" {
		t.Fatalf("invocation = %+v", inv)
	}
}

func TestRunnerCLIKinds(t *testing.T) {
	cases := []struct {
		kind    string
		command string
		args    []string
	}{
		{"opencode", "opencode", []string{"run", "-m", "deepseek/deepseek-v4-pro", "{briefing}"}},
		{"claude-code", "claude", []string{"-p", "--no-session-persistence", "--dangerously-skip-permissions", "--model", "claude-opus-4-8", "{briefing}"}},
		{"gemini-cli", "gemini", []string{"-p", "{briefing}", "-m", "gemini-3.1-pro-preview", "--approval-mode", "yolo", "--skip-trust"}},
		{"kimi-cli", "kimi", []string{"-p", "{briefing}", "-m", "kimi-code/kimi-for-coding"}},
		{"codex-cli", "codex", []string{"exec", "--sandbox", "danger-full-access", "{briefing}"}},
	}
	for _, tc := range cases {
		a, ok := Get(tc.kind)
		if !ok {
			t.Fatalf("%s not registered", tc.kind)
		}
		rs, ok := a.Runner()
		if !ok {
			t.Fatalf("%s should have a headless runner", tc.kind)
		}
		if rs.Command != tc.command {
			t.Fatalf("%s runner command = %q, want %q", tc.kind, rs.Command, tc.command)
		}
		if strings.Join(rs.Args, " ") != strings.Join(tc.args, " ") {
			t.Fatalf("%s runner args = %v, want %v", tc.kind, rs.Args, tc.args)
		}
	}
}

func TestRunnerNoHeadless(t *testing.T) {
	for _, kind := range []string{"antigravity", "claude-desktop", "codex-app", "cursor-cli"} {
		a, ok := Get(kind)
		if !ok {
			t.Fatalf("%s not registered", kind)
		}
		if _, ok := a.Runner(); ok {
			t.Fatalf("%s is GUI/unverified and must have no headless runner", kind)
		}
	}
}
