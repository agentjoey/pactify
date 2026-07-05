package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A conflicting merge must abort cleanly — never leave a half-merged, conflicted
// working tree (which then poisons later checkouts).
func TestMergeNoFFAbortsOnConflict(t *testing.T) {
	dir := t.TempDir()
	g := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	g("init", "-b", "main", "-q")
	g("config", "user.email", "t@t")
	g("config", "user.name", "t")
	write := func(s string) { os.WriteFile(filepath.Join(dir, "f.txt"), []byte(s), 0o644) }
	write("base\n")
	g("add", "-A")
	g("commit", "-qm", "base")
	g("checkout", "-qb", "feat")
	write("feat\n")
	g("commit", "-aqm", "feat")
	g("checkout", "-q", "main")
	write("main\n")
	g("commit", "-aqm", "main")

	if err := MergeNoFF(dir, "feat", "merge feat"); err == nil {
		t.Fatal("expected a conflict error from MergeNoFF")
	}
	if ch, _ := HasChanges(dir); ch {
		t.Fatal("tree left dirty/mid-merge after conflict — merge was not aborted")
	}
}

func tempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0o644)
	mustRun(t, dir, "add", "-A")
	mustRun(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func mustRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func TestPathIgnored(t *testing.T) {
	dir := tempRepo(t)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".pact/\n"), 0o644)
	// The `.pact/` pattern (trailing slash) matches only a directory, so create it
	// — mirrors the real caller, where the runtime dir's .pact already exists.
	os.MkdirAll(filepath.Join(dir, ".pact"), 0o755)
	if !PathIgnored(dir, ".pact") {
		t.Fatal("a gitignored path should report ignored")
	}
	if PathIgnored(dir, "base.txt") {
		t.Fatal("a tracked path should not report ignored")
	}
}

func TestPathTracked(t *testing.T) {
	dir := tempRepo(t)
	if !PathTracked(dir, "base.txt") {
		t.Fatal("a committed path should report tracked")
	}
	os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x"), 0o644)
	if PathTracked(dir, "untracked.txt") {
		t.Fatal("a never-added path should not report tracked")
	}
	os.MkdirAll(filepath.Join(dir, ".pact"), 0o755)
	os.WriteFile(filepath.Join(dir, ".pact", "log.jsonl"), []byte("{}\n"), 0o644)
	if PathTracked(dir, filepath.Join(dir, ".pact", "log.jsonl")) {
		t.Fatal(".pact/log.jsonl exists on disk but was never committed — should not report tracked")
	}
	mustRun(t, dir, "add", ".pact/log.jsonl")
	mustRun(t, dir, "commit", "-q", "-m", "track the ledger")
	if !PathTracked(dir, filepath.Join(dir, ".pact", "log.jsonl")) {
		t.Fatal("a committed .pact/log.jsonl should report tracked")
	}
}

// CheckoutOrCreate must surface git's actual diagnostic text (not a bare "exit
// status N") — this is what made the 2026-07-05 dogfood sandbox failure
// (P4/P5) needlessly hard to diagnose: the real cause was hidden behind an
// opaque error. Provoke a real, recognizable git failure (checking out a branch
// that's already checked out in another worktree) and assert the message
// survives.
func TestCheckoutOrCreateSurfacesGitOutput(t *testing.T) {
	dir := tempRepo(t)
	mustRun(t, dir, "branch", "elsewhere")
	wt := t.TempDir()
	mustRun(t, dir, "worktree", "add", wt, "elsewhere")
	err := CheckoutOrCreate(dir, "elsewhere")
	if err == nil {
		t.Fatal("checking out a branch held by another worktree should fail")
	}
	if !strings.Contains(err.Error(), "worktree") {
		t.Fatalf("error should carry git's actual diagnostic text (mentions the other worktree), got: %v", err)
	}
}

func TestCheckoutOrCreateAndBranch(t *testing.T) {
	dir := tempRepo(t)
	if err := CheckoutOrCreate(dir, "feat/x"); err != nil {
		t.Fatal(err)
	}
	if b, _ := CurrentBranch(dir); b != "feat/x" {
		t.Fatalf("branch = %q", b)
	}
	mustRun(t, dir, "checkout", "-q", "-")
	if err := CheckoutOrCreate(dir, "feat/x"); err != nil {
		t.Fatal(err)
	}
}

func TestHasChangesAndCommitAll(t *testing.T) {
	dir := tempRepo(t)
	if ch, _ := HasChanges(dir); ch {
		t.Fatal("clean tree should have no changes")
	}
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("y"), 0o644)
	if ch, _ := HasChanges(dir); !ch {
		t.Fatal("dirty tree should report changes")
	}
	if err := CommitAll(dir, "work"); err != nil {
		t.Fatal(err)
	}
	if ch, _ := HasChanges(dir); ch {
		t.Fatal("after commit, tree should be clean")
	}
}

func TestMergeNoFF(t *testing.T) {
	dir := tempRepo(t)
	base, _ := CurrentBranch(dir)
	CheckoutOrCreate(dir, "feat/x")
	os.WriteFile(filepath.Join(dir, "g.txt"), []byte("z"), 0o644)
	CommitAll(dir, "feat work")
	mustRun(t, dir, "checkout", "-q", base)
	if err := MergeNoFF(dir, "feat/x", "Merge feat/x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "g.txt")); err != nil {
		t.Fatal("merge did not bring feature file")
	}
}
