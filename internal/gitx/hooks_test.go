package gitx

import (
	"os"
	"path/filepath"
	"testing"
)

// installFailingHook writes a hook that always exits non-zero, so any pactify git
// command that runs human hooks would fail.
func installFailingHook(t *testing.T, dir, hook string) {
	t.Helper()
	p := filepath.Join(dir, ".git", "hooks", hook)
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho 'hook rejected'; exit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Machine-generated commits must NOT run the repo's human commit hooks
// (commitlint/lint-staged): a failing pre-commit hook would otherwise silently
// drop the worker's work, producing the checkpoint-phantom. CommitAll/CommitPaths
// must use --no-verify.
func TestCommitAllBypassesHooks(t *testing.T) {
	dir := tempRepo(t)
	installFailingHook(t, dir, "pre-commit")
	os.WriteFile(filepath.Join(dir, "work.txt"), []byte("real work\n"), 0o644)

	if err := CommitAll(dir, "pact: checkpoint"); err != nil {
		t.Fatalf("CommitAll must bypass the failing pre-commit hook (--no-verify): %v", err)
	}
	if dirty, _ := HasChanges(dir); dirty {
		t.Fatal("work was not committed (tree still dirty) — hook blocked the commit")
	}
}

func TestCommitPathsBypassesHooks(t *testing.T) {
	dir := tempRepo(t)
	installFailingHook(t, dir, "pre-commit")
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".pact/\n"), 0o644)

	if err := CommitPaths(dir, "chore", ".gitignore"); err != nil {
		t.Fatalf("CommitPaths must bypass the failing pre-commit hook: %v", err)
	}
}

// A merge commit must likewise bypass hooks (pre-merge-commit / commit-msg) — the
// merge verb is machine-generated.
func TestMergeNoFFBypassesHooks(t *testing.T) {
	dir := tempRepo(t)
	base, err := CurrentBranch(dir)
	if err != nil || base == "" {
		t.Fatalf("base branch: %v", err)
	}
	if err := CheckoutOrCreate(dir, "feat/x"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feature\n"), 0o644)
	if err := CommitAll(dir, "feature work"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "checkout", "-q", base)
	installFailingHook(t, dir, "pre-merge-commit")
	installFailingHook(t, dir, "commit-msg")

	if err := MergeNoFF(dir, "feat/x", "Merge feat/x"); err != nil {
		t.Fatalf("MergeNoFF must bypass merge hooks (--no-verify): %v", err)
	}
}
