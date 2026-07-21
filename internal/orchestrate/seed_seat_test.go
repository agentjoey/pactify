package orchestrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// In an isolated worktree (sandbox/parallel: LedgerDir points at the primary
// repo, Dir at the throwaway tree), the driver seeds the stint's seat into the
// worktree's .pact/seat — a second identity channel so a headless host that does
// NOT pass PACT_AGENT_ID through to its MCP server still resolves the acting
// seat (spec seat-identity §3.3). In-place runs must NOT touch the user's tree.
func TestSeedSeatOnlyInIsolatedWorktree(t *testing.T) {
	worktree := t.TempDir()
	primary := t.TempDir()
	os.MkdirAll(filepath.Join(worktree, ".pact"), 0o755)

	// Isolated: LedgerDir (primary) != Dir (worktree) → seed.
	iso := Options{Dir: worktree, LedgerDir: primary}
	iso.seedSeatIfIsolated("w")
	if b, err := os.ReadFile(filepath.Join(worktree, ".pact", "seat")); err != nil || string(b) != "w\n" {
		t.Fatalf("isolated worktree must be seeded with the stint seat: %q (%v)", b, err)
	}

	// A later stint on the SAME worktree (different seat) overwrites — the file
	// always reflects who is running now.
	iso.seedSeatIfIsolated("orch")
	if b, _ := os.ReadFile(filepath.Join(worktree, ".pact", "seat")); string(b) != "orch\n" {
		t.Fatalf("re-seed must overwrite with the current seat, got %q", b)
	}

	// In-place: LedgerDir unset → Dir is the user's tree → never seed.
	inplace := t.TempDir()
	os.MkdirAll(filepath.Join(inplace, ".pact"), 0o755)
	(Options{Dir: inplace}).seedSeatIfIsolated("w")
	if _, err := os.Stat(filepath.Join(inplace, ".pact", "seat")); !os.IsNotExist(err) {
		t.Fatal("in-place run must not seed a seat file into the user's working tree")
	}
}

// The seeded seat file MUST be git-excluded: user repos track .pact, so an
// un-excluded seat file would ride the worker's checkpoint into the feature
// branch and merge onto base — the committed-identity accident this prevents.
func TestSeedSeatExcludesFromGit(t *testing.T) {
	wt := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = wt
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	os.MkdirAll(filepath.Join(wt, ".pact"), 0o755)
	(Options{Dir: wt, LedgerDir: t.TempDir()}).seedSeatIfIsolated("w")

	excl, _ := os.ReadFile(filepath.Join(wt, ".git", "info", "exclude"))
	if !strings.Contains(string(excl), ".pact/seat") {
		t.Fatalf(".pact/seat must be git-excluded in the worktree, got:\n%s", excl)
	}
	// git must not see the seat file (excluded), even though .pact is tracked.
	out, _ := exec.Command("git", "-C", wt, "status", "--porcelain").CombinedOutput()
	if strings.Contains(string(out), ".pact/seat") {
		t.Fatalf("git must not track the seeded seat file:\n%s", out)
	}
}
