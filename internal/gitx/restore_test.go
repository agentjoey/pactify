package gitx

import (
	"os"
	"path/filepath"
	"testing"
)

// RestorePaths throws away working-tree modifications to the NAMED paths only —
// the surgical counterpart of DiscardUncommitted, used to hand git a clean
// checkout of exactly the files a caller has already snapshotted.
func TestRestorePaths(t *testing.T) {
	dir := tempRepo(t)
	os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("committed"), 0o644)
	mustRun(t, dir, "add", "keep.txt")
	mustRun(t, dir, "commit", "-q", "-m", "keep")

	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("dirty"), 0o644)
	os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("also dirty"), 0o644)
	os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("mine"), 0o644)

	if err := RestorePaths(dir, filepath.Join(dir, "base.txt")); err != nil {
		t.Fatalf("RestorePaths: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "base.txt")); string(b) != "x" {
		t.Fatalf("named path not restored to HEAD, got %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "keep.txt")); string(b) != "also dirty" {
		t.Fatalf("an UNNAMED tracked path must not be touched, got %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "untracked.txt")); err != nil {
		t.Fatalf("untracked files must survive: %v", err)
	}
}

// A pathspec git does not know is an error (not a silent no-op), so callers must
// filter with PathTracked before restoring.
func TestRestorePathsRejectsUnknownPath(t *testing.T) {
	dir := tempRepo(t)
	os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("mine"), 0o644)
	if err := RestorePaths(dir, "untracked.txt"); err == nil {
		t.Fatal("restoring an untracked pathspec should fail")
	}
}

// PathsDirty scopes the porcelain status to the given paths: dirt elsewhere in
// the tree must not read as "these files changed" (the guard that keeps a
// no-op ledger commit from being attempted, and a real one from being skipped).
func TestPathsDirty(t *testing.T) {
	dir := tempRepo(t)
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("committed"), 0o644)
	mustRun(t, dir, "add", "other.txt")
	mustRun(t, dir, "commit", "-q", "-m", "other")

	if PathsDirty(dir, filepath.Join(dir, "base.txt")) {
		t.Fatal("clean path reported dirty")
	}
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("changed"), 0o644)
	if PathsDirty(dir, filepath.Join(dir, "base.txt")) {
		t.Fatal("dirt in an unrelated file must not count")
	}
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("changed"), 0o644)
	if !PathsDirty(dir, filepath.Join(dir, "base.txt")) {
		t.Fatal("modified path should report dirty")
	}
}
