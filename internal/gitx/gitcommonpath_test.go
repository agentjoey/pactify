package gitx

import (
	"path/filepath"
	"testing"
)

// normPath resolves symlinks in the (existing) parent dir so paths reported by
// git (physical, e.g. /private/var on macOS) compare equal to paths built from
// t.TempDir() (logical, /var). The leaf itself may not exist yet (lock files).
func normPath(t *testing.T, p string) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return p
	}
	return filepath.Join(d, filepath.Base(p))
}

// The base-integration lock must be ONE file per repo: GitCommonPath resolves
// to the same path from the main tree and a linked worktree, while GitPath
// (per-worktree gitdir — the right scope for the per-worktree ledger lock)
// resolves to different ones.
func TestGitCommonPathSharedAcrossWorktrees(t *testing.T) {
	repo := initRepo(t)
	wt := filepath.Join(t.TempDir(), "wt-common")
	if err := AddWorktree(repo, wt, "feat-lock", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	mainCommon, err := GitCommonPath(repo, "pactify-base.lock")
	if err != nil {
		t.Fatalf("GitCommonPath(main): %v", err)
	}
	wtCommon, err := GitCommonPath(wt, "pactify-base.lock")
	if err != nil {
		t.Fatalf("GitCommonPath(worktree): %v", err)
	}
	if normPath(t, mainCommon) != normPath(t, wtCommon) {
		t.Fatalf("GitCommonPath must resolve to ONE shared file:\n  main: %s\n  wt:   %s", mainCommon, wtCommon)
	}

	mainPer, err := GitPath(repo, "pactify-base.lock")
	if err != nil {
		t.Fatalf("GitPath(main): %v", err)
	}
	wtPer, err := GitPath(wt, "pactify-base.lock")
	if err != nil {
		t.Fatalf("GitPath(worktree): %v", err)
	}
	if normPath(t, mainPer) == normPath(t, wtPer) {
		t.Fatalf("GitPath from a linked worktree should be per-worktree, got same file: %s", mainPer)
	}
	// Sanity: in the main tree the two resolvers agree (.git IS the common dir).
	if normPath(t, mainCommon) != normPath(t, mainPer) {
		t.Fatalf("main tree: GitCommonPath %s != GitPath %s", mainCommon, mainPer)
	}
}
