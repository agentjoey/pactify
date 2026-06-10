package agent

import (
	"os"
	"strings"
	"testing"
)

func TestBriefingMentionsSeatRolesAndMCP(t *testing.T) {
	b := briefing("opencode", "worker")
	for _, want := range []string{"seat `opencode`", "worker", "MCP", "status", "join", "cannot self-accept"} {
		if !strings.Contains(b, want) {
			t.Fatalf("briefing missing %q:\n%s", want, b)
		}
	}
	// the fiddly backtick-concatenation must yield a balanced code fence
	if n := strings.Count(b, "```"); n != 2 {
		t.Fatalf("briefing code fence not balanced: found %d ``` markers", n)
	}
}

func TestRenderJSONKindReturnsBlockAndSnippet(t *testing.T) {
	entry, cfg, err := Render("opencode", "opencode", "worker", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entry, "seat `opencode`") {
		t.Fatalf("entry block wrong:\n%s", entry)
	}
	if !strings.Contains(cfg, `"type": "local"`) {
		t.Fatalf("config snippet wrong:\n%s", cfg)
	}
}

func TestRenderDesktopAppHasNoEntry(t *testing.T) {
	entry, cfg, err := Render("antigravity", "antigravity", "worker", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if entry != "" {
		t.Fatalf("desktop app should render no entry block, got:\n%s", entry)
	}
	if !strings.Contains(cfg, "--project") {
		t.Fatalf("desktop snippet should include --project:\n%s", cfg)
	}
}

func TestRenderUnknownKindErrors(t *testing.T) {
	if _, _, err := Render("nope", "s", "r", "/r"); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestWireJSONKindWritesConfigAndEntry(t *testing.T) {
	dir := t.TempDir()
	start, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(start) })
	os.Chdir(dir)

	if err := Wire("opencode", "opencode", "worker", "/repo"); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile("opencode.json")
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if !strings.Contains(string(cfg), `"pact"`) {
		t.Fatalf("config missing pact server:\n%s", cfg)
	}
	entry, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatalf("entry not written: %v", err)
	}
	if !strings.Contains(string(entry), "seat `opencode`") {
		t.Fatalf("entry missing briefing:\n%s", entry)
	}
}

func TestWireTOMLKindDoesNotWriteConfig(t *testing.T) {
	dir := t.TempDir()
	start, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(start) })
	os.Chdir(dir)
	if err := Wire("codex-cli", "codex", "worker", "/repo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(".codex/config.toml"); err == nil {
		t.Fatal("TOML kind must be doc-only — no config file should be written")
	}
	if _, err := os.Stat("AGENTS.md"); err != nil {
		t.Fatalf("entry file should still be baked for codex-cli: %v", err)
	}
}
