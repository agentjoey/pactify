package sub

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func git(t *testing.T, dir string, args ...string) { t.Helper(); gitOut(t, dir, args...) }
func write(t *testing.T, dir, path, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "t@t.t")
	git(t, dir, "config", "user.name", "t")
	write(t, dir, "a.txt", "one\n")
	write(t, dir, "b.txt", "one\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "base")
	return dir
}

func TestDiffSinceExcludesPreexistingChanges(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.txt", "one\nuser edit\n")
	write(t, dir, "notes.tmp", "user untracked\n")
	before := gitOut(t, dir, "status", "--porcelain=v1")
	snap, err := TakeSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := gitOut(t, dir, "status", "--porcelain=v1"); got != before {
		t.Fatalf("snapshot changed workspace: %q -> %q", before, got)
	}
	write(t, dir, "b.txt", "one\ncallee edit\ncallee edit 2\n")
	write(t, dir, "new.go", "package x\n")
	d, err := DiffSince(dir, snap)
	if err != nil {
		t.Fatal(err)
	}
	want := Diff{Files: []FileDelta{{Path: "b.txt", Added: 2}, {Path: "new.go", Added: 1, New: true}}, Added: 3}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("diff=%+v want=%+v", d, want)
	}
	if got := gitOut(t, dir, "stash", "list"); got != "" {
		t.Fatalf("stash list changed: %q", got)
	}
}

func TestDiffSinceCleanStagedDeletedAndOddPaths(t *testing.T) {
	dir := repo(t)
	snap, err := TakeSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Base != gitOut(t, dir, "rev-parse", "HEAD") {
		t.Fatalf("clean base=%s", snap.Base)
	}
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "b.txt", "one\nstaged\n")
	git(t, dir, "add", "b.txt")
	write(t, dir, "odd\tname\n.txt", "x\ny\n")
	write(t, dir, "binary.dat", "x\x00y\n")
	d, err := DiffSince(dir, snap)
	if err != nil {
		t.Fatal(err)
	}
	want := Diff{Files: []FileDelta{{Path: "a.txt", Deleted: 1}, {Path: "b.txt", Added: 1}, {Path: "binary.dat", New: true}, {Path: "odd\tname\n.txt", Added: 2, New: true}}, Added: 3, Deleted: 1}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("diff=%+v want=%+v", d, want)
	}
}

func TestDiffSincePreservesStagedBaseline(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.txt", "one\nuser staged\n")
	git(t, dir, "add", "a.txt")
	write(t, dir, "a.txt", "one\nuser staged\nuser unstaged\n")
	snap, err := TakeSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "one\nuser staged\nuser unstaged\ncallee\n")
	d, err := DiffSince(dir, snap)
	if err != nil {
		t.Fatal(err)
	}
	if d.Added != 1 || d.Deleted != 0 || len(d.Files) != 1 {
		t.Fatalf("diff=%+v", d)
	}
	if got := gitOut(t, dir, "diff", "--cached", "--numstat"); got != "1\t0\ta.txt" {
		t.Fatalf("index changed: %q", got)
	}
}

func TestDiffErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := TakeSnapshot(dir); err == nil {
		t.Fatal("non-repo snapshot succeeded")
	}
	if _, err := DiffSince(dir, Snapshot{Base: "HEAD"}); err == nil {
		t.Fatal("non-repo diff succeeded")
	}
}

func TestDiffSinceExcludesPreexistingUntrackedAfterStaging(t *testing.T) {
	dir := repo(t)
	write(t, dir, "notes.tmp", "user untracked\n")
	snap, err := TakeSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "notes.tmp")
	write(t, dir, "new.go", "package x\n")
	d, err := DiffSince(dir, snap)
	if err != nil {
		t.Fatal(err)
	}
	want := Diff{Files: []FileDelta{{Path: "new.go", Added: 1, New: true}}, Added: 1}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("diff=%+v want=%+v", d, want)
	}
}

func TestDiffSinceSubdirectoryUsesRepositoryPaths(t *testing.T) {
	dir := repo(t)
	subdir := filepath.Join(dir, "nested")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "outside.tmp", "user\n")
	write(t, subdir, "notes.tmp", "user\n")
	snap, err := TakeSnapshot(subdir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "b.txt", "one\ncallee\n")
	write(t, dir, "new-root.txt", "root\n")
	write(t, subdir, "new-child.txt", "child\n")
	d, err := DiffSince(subdir, snap)
	if err != nil {
		t.Fatal(err)
	}
	want := Diff{Files: []FileDelta{{Path: "b.txt", Added: 1}, {Path: "nested/new-child.txt", Added: 1, New: true}, {Path: "new-root.txt", Added: 1, New: true}}, Added: 3}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("diff=%+v want=%+v", d, want)
	}
}
