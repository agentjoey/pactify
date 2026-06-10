package agent

import (
	"strings"
	"testing"
)

func TestKindsSortedAndComplete(t *testing.T) {
	got := strings.Join(Kinds(), ",")
	want := "antigravity,claude-code,claude-desktop,codex-app,codex-cli,gemini-cli,opencode"
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

func TestUnknownKind(t *testing.T) {
	if _, ok := Get("nope"); ok {
		t.Fatal("unknown kind should not resolve")
	}
}
