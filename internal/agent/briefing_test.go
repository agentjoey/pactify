package agent

import (
	"os"
	"strings"
	"testing"
)

func TestBriefingIsSeatAgnosticAndMentionsBindingAndMCP(t *testing.T) {
	b := briefing()
	for _, want := range []string{"pact protocol", "pactify seat use", ".pact/seat", "MCP", "status", "join", "cannot self-accept"} {
		if !strings.Contains(b, want) {
			t.Fatalf("briefing missing %q:\n%s", want, b)
		}
	}
	// It must name no seat and pin no identity (the whole point of seat-agnostic).
	for _, bad := range []string{"seat `opencode`", "PACT_AGENT_ID=opencode"} {
		if strings.Contains(b, bad) {
			t.Fatalf("briefing must be seat-agnostic, found %q", bad)
		}
	}
	// two code fences (identity bash + shell-fallback bash) must be balanced
	if n := strings.Count(b, "```"); n != 4 {
		t.Fatalf("briefing code fences not balanced: found %d ``` markers", n)
	}
}

func TestRenderJSONKindReturnsBlockAndSnippet(t *testing.T) {
	entry, cfg, err := Render("opencode", "opencode", "worker", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entry, "pact protocol") || !strings.Contains(entry, "pactify seat use") {
		t.Fatalf("entry block must be seat-agnostic onboarding:\n%s", entry)
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
	if !strings.Contains(string(entry), "pact protocol") || !strings.Contains(string(entry), "pactify seat use") {
		t.Fatalf("entry missing seat-agnostic briefing:\n%s", entry)
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

func TestRenderDocCoversEveryKind(t *testing.T) {
	doc := RenderDoc()
	for _, k := range Kinds() {
		if !strings.Contains(doc, "## "+k) {
			t.Fatalf("doc missing section for kind %q", k)
		}
	}
}

func TestDocFileInSync(t *testing.T) {
	b, err := os.ReadFile("../../docs/agent-onboarding.md")
	if err != nil {
		t.Fatalf("docs/agent-onboarding.md missing — run `pactify agent docs`: %v", err)
	}
	if string(b) != RenderDoc() {
		t.Fatal("docs/agent-onboarding.md is stale — regenerate with `pactify agent docs`")
	}
}

// 2026-07-22 diagnosis REVERSED (spec seat-identity §5 P1): two seats of the
// same project-scoped kind wired into one repo must NOT collide. The entry
// block is seat-agnostic (byte-identical for any seat → repeat writes are
// idempotent), and the config carries no pinned per-seat PACT_AGENT_ID.
func TestTwoSameKindSeatsDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	start, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(start) })
	os.Chdir(dir)

	if err := Wire("claude-code", "lead", "orchestrator,reviewer", "/repo"); err != nil {
		t.Fatal(err)
	}
	entryAfterLead, _ := os.ReadFile("CLAUDE.md")
	if err := Wire("claude-code", "reviewer", "reviewer", "/repo"); err != nil {
		t.Fatal(err)
	}
	entryAfterRev, _ := os.ReadFile("CLAUDE.md")

	// The lead's wiring is NOT lost — the block is seat-agnostic, so the second
	// write is byte-identical (idempotent), not an overwrite.
	if string(entryAfterLead) != string(entryAfterRev) {
		t.Fatalf("entry block must be seat-agnostic (idempotent across seats):\n--- after lead ---\n%s\n--- after reviewer ---\n%s", entryAfterLead, entryAfterRev)
	}
	entry := string(entryAfterRev)
	for _, bad := range []string{"seat `lead`", "seat `reviewer`", "PACT_AGENT_ID=lead", "PACT_AGENT_ID=reviewer"} {
		if strings.Contains(entry, bad) {
			t.Fatalf("entry block must not pin a seat identity, found %q:\n%s", bad, entry)
		}
	}
	// It must still tell the agent HOW to bind an identity.
	if !strings.Contains(entry, "pactify seat use") {
		t.Fatalf("entry block must document identity binding:\n%s", entry)
	}
	// Config: no pinned per-seat identity. The pact server entry may carry an env
	// pass-through token (${PACT_AGENT_ID...}) but never a literal seat id.
	cfg, _ := os.ReadFile(".mcp.json")
	for _, bad := range []string{`"lead"`, `"reviewer"`} {
		if strings.Contains(string(cfg), bad) {
			t.Fatalf("config must not pin a seat id, found %s:\n%s", bad, cfg)
		}
	}
}
