package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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
