package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyEntryBlock is an entry file exactly as pactify baked it BEFORE the
// managed block carried a kind-attribution line: the marker is present but says
// nothing about WHICH kind was wired. Used by the back-compat tests below.
const legacyEntryBlock = "# hand-written notes\n\n" +
	"<!-- pact:begin (managed by pactify — edit outside this block) -->\n" +
	"# pact protocol\n\nThis repo uses the **pact protocol** (v1).\n" +
	"<!-- pact:end -->\n"

// wiredSet folds ProbeWiring into kind→wired for compact assertions.
func wiredSet(dir string) map[string]bool {
	m := map[string]bool{}
	for _, r := range ProbeWiring(dir) {
		m[r.Kind] = r.Wired
	}
	return m
}

// assertWiring checks the wired/unwired expectation for the named kinds only.
func assertWiring(t *testing.T, dir string, want map[string]bool) {
	t.Helper()
	got := wiredSet(dir)
	for k, w := range want {
		if got[k] != w {
			t.Errorf("kind %q wired = %v, want %v (row: %+v)", k, got[k], w, findStatus(t, ProbeWiring(dir), k))
		}
	}
}

// THE BUG: AGENTS.md is the entry file of opencode, codex-cli, kimi-cli,
// cursor-cli and codex-app. Wiring ONLY opencode used to bake a kind-agnostic
// marker into AGENTS.md, which made all four co-tenants report
// `Wired: true, Detail: "entry AGENTS.md"` even though none was ever configured.
func TestWireOpencodeDoesNotWireItsAGENTSCoTenants(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := WireAt(dir, "opencode", "seat", "worker", dir); err != nil {
		t.Fatal(err)
	}
	assertWiring(t, dir, map[string]bool{
		"opencode":   true,  // genuinely wired
		"codex-cli":  false, // doc-only co-tenant of AGENTS.md — never configured
		"kimi-cli":   false,
		"cursor-cli": false,
		"codex-app":  false,
	})
}

// The same false positive on the other shared entry file: GEMINI.md is shared by
// gemini-cli and antigravity.
func TestWireGeminiCliDoesNotWireAntigravity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := WireAt(dir, "gemini-cli", "seat", "worker", dir); err != nil {
		t.Fatal(err)
	}
	assertWiring(t, dir, map[string]bool{"gemini-cli": true, "antigravity": false})
}

// …and vice versa: wiring antigravity (machine-global config under a fake HOME)
// must not make gemini-cli look wired.
func TestWireAntigravityDoesNotWireGeminiCli(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := WireAt(dir, "antigravity", "seat", "worker", dir); err != nil {
		t.Fatal(err)
	}
	assertWiring(t, dir, map[string]bool{"antigravity": true, "gemini-cli": false})
}

// Regression guard: a config-writing kind that IS genuinely wired keeps
// reporting wired — via its own config file, not via the shared entry.
func TestWiredConfigKindsStillReportWired(t *testing.T) {
	for _, k := range []string{"opencode", "claude-code", "gemini-cli", "cursor-cli", "kimi-cli", "claude-desktop"} {
		t.Run(k, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			dir := t.TempDir()
			if err := WireAt(dir, k, "seat", "worker", dir); err != nil {
				t.Fatal(err)
			}
			row := findStatus(t, ProbeWiring(dir), k)
			if !row.Wired {
				t.Fatalf("%s was just wired but reports unwired: %+v", k, row)
			}
			if !strings.HasPrefix(row.Detail, "config ") {
				t.Fatalf("%s should be proved by its config file, got detail %q", k, row.Detail)
			}
		})
	}
}

// A doc-only kind writes no config, so the entry file is its ONLY wiring signal —
// it must still report wired after `agent add`, now attributed to it by name.
func TestWireDocOnlyKindReportsWiredViaEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := WireAt(dir, "codex-cli", "seat", "worker", dir); err != nil {
		t.Fatal(err)
	}
	row := findStatus(t, ProbeWiring(dir), "codex-cli")
	if !row.Wired || row.Detail != "entry AGENTS.md" {
		t.Fatalf("codex-cli should be wired via its entry file: %+v", row)
	}
	// …and its doc-only co-tenant codex-app must not ride along.
	assertWiring(t, dir, map[string]bool{"codex-app": false, "opencode": false})
}

// Wiring several kinds into one shared entry file accumulates the attribution
// (it is a set, not last-writer-wins).
func TestWireAtAccumulatesSharedEntryKinds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	for _, k := range []string{"opencode", "codex-cli"} {
		if err := WireAt(dir, k, "seat", "worker", dir); err != nil {
			t.Fatal(err)
		}
	}
	assertWiring(t, dir, map[string]bool{
		"opencode": true, "codex-cli": true,
		"kimi-cli": false, "cursor-cli": false, "codex-app": false,
	})
	b, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "pact:begin"); n != 1 {
		t.Fatalf("entry must carry exactly one managed block, found %d:\n%s", n, b)
	}
}

// Re-wiring the same kind is byte-idempotent (the seat-agnostic block plus a
// stable, sorted attribution line).
func TestWireAtEntryIsByteIdempotent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := WireAt(dir, "opencode", "lead", "worker", dir); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err := WireAt(dir, "opencode", "second", "reviewer", dir); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if string(first) != string(second) {
		t.Fatalf("re-wiring the same kind must be byte-identical:\n--- 1 ---\n%s\n--- 2 ---\n%s", first, second)
	}
}

// BACK-COMPAT (decision): a legacy block carries no attribution, so it is
// credited ONLY to kinds for which nothing else on disk could ever prove wiring:
//   - entry-only kinds (doc-only / no MCP config path) — the entry file is their
//     sole signal, so refusing it would un-wire them with no way to tell;
//   - the sole tenant of that entry file — unambiguous by construction.
//
// Config-writing kinds that share the file fall back to their own config probe,
// which is exactly what `agent add` wrote for them. So AGENTS.md's legacy block
// credits codex-cli and codex-app (inherently indistinguishable) and nobody else.
func TestLegacyBlockCreditsOnlyEntryOnlyKinds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(legacyEntryBlock), 0o644); err != nil {
		t.Fatal(err)
	}
	assertWiring(t, dir, map[string]bool{
		"codex-cli": true, "codex-app": true, // doc-only: entry file is the only possible signal
		"opencode": false, "kimi-cli": false, "cursor-cli": false, // config-writing: must show a config
	})
}

// The sole tenant of an entry file is unambiguous, so a legacy CLAUDE.md block
// still credits claude-code (no behavior change for that shape).
func TestLegacyBlockCreditsSoleTenantEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(legacyEntryBlock), 0o644); err != nil {
		t.Fatal(err)
	}
	assertWiring(t, dir, map[string]bool{"claude-code": true})
}

// GEMINI.md is shared by two config-writing kinds, so a legacy block there
// credits neither — each is proved by the config `agent add` wrote for it.
func TestLegacyBlockCreditsNeitherGeminiKind(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "GEMINI.md"), []byte(legacyEntryBlock), 0o644); err != nil {
		t.Fatal(err)
	}
	assertWiring(t, dir, map[string]bool{"gemini-cli": false, "antigravity": false})
}

// Re-wiring over a legacy block upgrades it to an attributed one: the kind you
// actually wired is recorded, and the legacy co-tenant credit disappears.
func TestWireAtUpgradesLegacyBlock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(legacyEntryBlock), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WireAt(dir, "opencode", "seat", "worker", dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(string(b), "hand-written notes") {
		t.Fatalf("re-bake must preserve content outside the block:\n%s", b)
	}
	assertWiring(t, dir, map[string]bool{
		"opencode": true, "codex-cli": false, "codex-app": false,
	})
}

// The attribution line is machine-readable and round-trips through the managed
// block (it must not trip pact.BakeManagedBlock's marker guard or stripBlock).
func TestEntryKindsRoundTrip(t *testing.T) {
	body := entryBody([]string{"codex-cli", "opencode"})
	blk := "<!-- pact:begin (managed) -->\n" + body + "\n<!-- pact:end -->\n"
	kinds, attributed := entryKinds(blk)
	if !attributed {
		t.Fatalf("block must be attributed:\n%s", blk)
	}
	if strings.Join(kinds, ",") != "codex-cli,opencode" {
		t.Fatalf("kinds = %v, want [codex-cli opencode]", kinds)
	}
	if _, attributed := entryKinds(legacyEntryBlock); attributed {
		t.Fatal("legacy block must not parse as attributed")
	}
	if _, attributed := entryKinds("# no pact block here\n"); attributed {
		t.Fatal("a file with no managed block must not parse as attributed")
	}
}
