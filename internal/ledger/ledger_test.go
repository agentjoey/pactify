package ledger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/paths"
)

// The whole point of this package: a project-scoped lookup answers about the
// repo it was handed, even when the process is pinned elsewhere by an absolute
// PACT_DIR. paths.LogIn does the opposite (correctly, for its own callers).
func TestPathIgnoresAbsolutePactDirOverride(t *testing.T) {
	other := t.TempDir()
	t.Setenv("PACT_DIR", filepath.Join(other, ".pact"))

	repo := t.TempDir()
	want := filepath.Join(repo, ".pact", "log.jsonl")

	if got := Path(repo); got != want {
		t.Errorf("Path(%q) = %q, want %q", repo, got, want)
	}
	// Guard the contrast that motivates this package: if paths.LogIn ever stopped
	// honouring an absolute PACT_DIR, this package's reason to exist would change
	// and the doc comment would be lying.
	if got := paths.LogIn(repo); got == want {
		t.Errorf("paths.LogIn(%q) = %q — it is expected to be process-scoped and return the PACT_DIR override instead", repo, got)
	}
}

func TestReadFoldsMissingLogToEmpty(t *testing.T) {
	evs, err := Read(t.TempDir())
	if err != nil {
		t.Fatalf("Read on an uninitialized project must not error: %v", err)
	}
	if len(evs) != 0 {
		t.Errorf("want no events, got %d", len(evs))
	}
}

func TestReadReturnsTheProjectsOwnEvents(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(Dir(repo), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"event_id":"a1","ts":"2026-01-01T00:00:00Z","agent_id":"w","role":"worker","event_type":"note","task_id":"t1"}` + "\n"
	if err := os.WriteFile(Path(repo), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	// A different repo is pinned by env; Read must still answer about `repo`.
	t.Setenv("PACT_DIR", filepath.Join(t.TempDir(), ".pact"))

	evs, err := Read(repo)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(evs) != 1 || evs[0].EventID != "a1" {
		t.Fatalf("want the project's own single event, got %+v", evs)
	}
}
