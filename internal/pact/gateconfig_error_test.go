package pact

import (
	"os"
	"path/filepath"
	"testing"
)

// GateConfig must distinguish "no gate configured" (missing log → ("", false,
// nil)) from "log unreadable" (→ error): its consumer is orchestrate's
// pre-merge hard gate, and a read error swallowed as "unconfigured" would
// silently degrade a configured gate to the type-default fallback.
func TestGateConfigDistinguishesErrorFromAbsent(t *testing.T) {
	t.Setenv("PACT_DIR", "")

	// No .pact/log.jsonl at all: absent, not an error.
	if g, ok, err := At(t.TempDir()).GateConfig(); g != "" || ok || err != nil {
		t.Fatalf("missing log: GateConfig = (%q,%v,%v), want (\"\",false,nil)", g, ok, err)
	}

	// Corrupt log: error, never a silent "unconfigured".
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".pact"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pact", "log.jsonl"), []byte("{not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := At(dir).GateConfig(); err == nil || ok {
		t.Fatalf("corrupt log: GateConfig = (_,%v,%v), want an error and ok=false", ok, err)
	}
}
