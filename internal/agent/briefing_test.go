package agent

import (
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
