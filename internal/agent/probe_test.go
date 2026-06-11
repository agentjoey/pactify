package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// findStatus returns the WiringStatus for kind from a ProbeWiring slice.
func findStatus(t *testing.T, rows []WiringStatus, kind string) WiringStatus {
	t.Helper()
	for _, r := range rows {
		if r.Kind == kind {
			return r
		}
	}
	t.Fatalf("kind %q not in probe rows: %+v", kind, rows)
	return WiringStatus{}
}

// TestProbeWiringUnwired: a bare dir reports every kind not wired, and the rows
// cover exactly the sorted registry kinds.
func TestProbeWiringUnwired(t *testing.T) {
	// Isolate HOME so global kinds don't read a real machine config.
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	rows := ProbeWiring(dir)
	if len(rows) != len(Kinds()) {
		t.Fatalf("rows = %d want %d (one per kind)", len(rows), len(Kinds()))
	}
	for _, r := range rows {
		if r.Wired {
			t.Fatalf("kind %q should be unwired in a bare dir: %+v", r.Kind, r)
		}
		if r.Detail != "not wired" {
			t.Fatalf("kind %q unwired detail = %q want 'not wired'", r.Kind, r.Detail)
		}
	}
}

// TestProbeWiringConfigDetected: an opencode.json carrying the pact key marks
// the opencode row wired via the config probe.
func TestProbeWiringConfigDetected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{"mcp":{"pact":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	row := findStatus(t, ProbeWiring(dir), "opencode")
	if !row.Wired {
		t.Fatalf("opencode should be wired: %+v", row)
	}
	if row.Detail != "config opencode.json" {
		t.Fatalf("detail = %q want 'config opencode.json'", row.Detail)
	}
	if row.Global || row.DocOnly {
		t.Fatalf("opencode is project-scoped JSON: %+v", row)
	}
}

// TestProbeWiringEntryDetected: a CLAUDE.md carrying the pact:begin marker marks
// the claude-code row wired via the entry probe.
func TestProbeWiringEntryDetected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# notes\n<!-- pact:begin -->\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	row := findStatus(t, ProbeWiring(dir), "claude-code")
	if !row.Wired {
		t.Fatalf("claude-code should be wired via entry: %+v", row)
	}
	if row.Detail != "entry CLAUDE.md" {
		t.Fatalf("detail = %q want 'entry CLAUDE.md'", row.Detail)
	}
}

// TestProbeWiringIgnoresUnwiredEntry: an entry file WITHOUT the marker is not
// wiring — the probe is content-aware, not presence-based.
func TestProbeWiringIgnoresUnwiredEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# just notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	row := findStatus(t, ProbeWiring(dir), "claude-code")
	if row.Wired {
		t.Fatalf("CLAUDE.md without pact:begin must not count: %+v", row)
	}
}

// TestProbeWiringGlobalKind: a global-kind config under a fake HOME is detected
// by the config probe, and the row is flagged Global.
func TestProbeWiringGlobalKind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// claude-desktop config lives at ~/Library/Application Support/Claude/.
	cfg := filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(`{"mcpServers":{"pact":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	row := findStatus(t, ProbeWiring(t.TempDir()), "claude-desktop")
	if !row.Wired {
		t.Fatalf("claude-desktop global config should be detected: %+v", row)
	}
	if !row.Global {
		t.Fatalf("claude-desktop must be flagged Global: %+v", row)
	}
}

// TestProbeWiringCodexDocOnly: codex (TOML) kinds are flagged DocOnly; a TOML
// config carrying the mcp_servers.pact table is detected via the config probe.
func TestProbeWiringCodexDocOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	row := findStatus(t, ProbeWiring(dir), "codex-cli")
	if !row.DocOnly {
		t.Fatalf("codex-cli must be flagged DocOnly: %+v", row)
	}
	// Now bake a TOML config and confirm the substring marker probe.
	cfg := filepath.Join(dir, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("[mcp_servers.pact]\ncommand = \"pactify\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	row = findStatus(t, ProbeWiring(dir), "codex-cli")
	if !row.Wired {
		t.Fatalf("codex-cli with mcp_servers.pact table should be wired: %+v", row)
	}
}
