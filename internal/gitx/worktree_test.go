package gitx

import (
	"os"
	"path/filepath"
	"testing"
)

// initRepo creates a temp git repo with one commit on main and returns its path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		if _, err := run(dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(dir, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := run(dir, "commit", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAddWorktree_CreatesBranchAndDir(t *testing.T) {
	repo := initRepo(t)
	wt := filepath.Join(t.TempDir(), "wt-fa")
	if err := AddWorktree(repo, wt, "feat-a", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	// The worktree dir exists and is checked out on the new branch.
	if _, err := os.Stat(filepath.Join(wt, "README.md")); err != nil {
		t.Fatalf("worktree dir missing README: %v", err)
	}
	br, err := CurrentBranch(wt)
	if err != nil {
		t.Fatal(err)
	}
	if br != "feat-a" {
		t.Fatalf("worktree branch = %q, want feat-a", br)
	}
	if !BranchExists(repo, "feat-a") {
		t.Fatal("feat-a should exist after AddWorktree")
	}
}

func TestAddWorktree_ExistingBranch(t *testing.T) {
	repo := initRepo(t)
	if err := CheckoutOrCreate(repo, "feat-b"); err != nil {
		t.Fatal(err)
	}
	if err := Checkout(repo, "main"); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "wt-fb")
	if err := AddWorktree(repo, wt, "feat-b", "main"); err != nil {
		t.Fatalf("AddWorktree existing branch: %v", err)
	}
	if br, _ := CurrentBranch(wt); br != "feat-b" {
		t.Fatalf("branch = %q, want feat-b", br)
	}
}

func TestRemoveWorktree(t *testing.T) {
	repo := initRepo(t)
	wt := filepath.Join(t.TempDir(), "wt-rm")
	if err := AddWorktree(repo, wt, "feat-c", "main"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveWorktree(repo, wt); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir should be gone, stat err = %v", err)
	}
}

// Two worktrees for two different branches coexist — the basis for running two
// features' agents concurrently without colliding in the shared tree.
func TestAddWorktree_TwoConcurrent(t *testing.T) {
	repo := initRepo(t)
	base := t.TempDir()
	wtA := filepath.Join(base, "a")
	wtB := filepath.Join(base, "b")
	if err := AddWorktree(repo, wtA, "feat-a", "main"); err != nil {
		t.Fatal(err)
	}
	if err := AddWorktree(repo, wtB, "feat-b", "main"); err != nil {
		t.Fatal(err)
	}
	if br, _ := CurrentBranch(wtA); br != "feat-a" {
		t.Errorf("wtA branch = %q", br)
	}
	if br, _ := CurrentBranch(wtB); br != "feat-b" {
		t.Errorf("wtB branch = %q", br)
	}
}
