package paths

import (
	"os"
	"path/filepath"
	"testing"
)

// Seat identity resolves through a three-level chain (spec seat-identity §3.1):
// process env PACT_AGENT_ID > untracked .pact/seat file > unresolved. The source
// is reported so `pactify seat` can explain WHERE the identity came from.

func TestAgentIDIn_EnvWinsOverFile(t *testing.T) {
	base := t.TempDir()
	writeSeatFile(t, base, "from-file")
	t.Setenv("PACT_AGENT_ID", "from-env")
	if id, src := AgentIDIn(base); id != "from-env" || src != SourceEnv {
		t.Fatalf("env must win: got (%q,%q), want (from-env,%q)", id, src, SourceEnv)
	}
}

func TestAgentIDIn_FileWhenEnvEmpty(t *testing.T) {
	base := t.TempDir()
	writeSeatFile(t, base, "from-file")
	t.Setenv("PACT_AGENT_ID", "")
	if id, src := AgentIDIn(base); id != "from-file" || src != SourceFile {
		t.Fatalf("file must resolve when env empty: got (%q,%q), want (from-file,%q)", id, src, SourceFile)
	}
}

func TestAgentIDIn_UnresolvedWhenBothAbsent(t *testing.T) {
	base := t.TempDir()
	t.Setenv("PACT_AGENT_ID", "")
	if id, src := AgentIDIn(base); id != "" || src != SourceUnresolved {
		t.Fatalf("must be unresolved: got (%q,%q), want (\"\",%q)", id, src, SourceUnresolved)
	}
}

// A whitespace-only or empty seat file is not an identity — it resolves as
// unresolved, never a blank seat id (which would corrupt the ledger).
func TestAgentIDIn_BlankFileIsUnresolved(t *testing.T) {
	base := t.TempDir()
	writeSeatFile(t, base, "  \n")
	t.Setenv("PACT_AGENT_ID", "")
	if id, src := AgentIDIn(base); id != "" || src != SourceUnresolved {
		t.Fatalf("blank file must be unresolved: got (%q,%q)", id, src)
	}
}

// The file's surrounding whitespace/newline is trimmed (it's a single-line file).
func TestAgentIDIn_FileTrimmed(t *testing.T) {
	base := t.TempDir()
	writeSeatFile(t, base, "lead\n")
	t.Setenv("PACT_AGENT_ID", "")
	if id, _ := AgentIDIn(base); id != "lead" {
		t.Fatalf("seat id must be trimmed, got %q", id)
	}
}

func writeSeatFile(t *testing.T, base, content string) {
	t.Helper()
	dir := DirIn(base)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seat"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
