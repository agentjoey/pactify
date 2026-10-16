package sub

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func unbornRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "t@t.t")
	git(t, dir, "config", "user.name", "t")
	return dir
}

func TestDiffSinceUnbornRepository(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "still unborn", true: "first commit during job"}[commit], func(t *testing.T) {
			dir := unbornRepo(t)
			write(t, dir, ".gitignore", "ignored\n")
			write(t, dir, "ignored", "ignored\n")
			write(t, dir, "staged.txt", "before\n")
			write(t, dir, "removed.txt", "removed\n")
			git(t, dir, "add", "staged.txt", "removed.txt")
			// A working file named HEAD must not be mistaken for a revision.
			write(t, dir, "HEAD", "user file\n")
			before := gitOut(t, dir, "status", "--porcelain=v1")
			snap, err := TakeSnapshot(dir)
			if err != nil {
				t.Fatal(err)
			}
			if !snap.EmptyTree || snap.Base == "" || gitOut(t, dir, "cat-file", "-t", snap.Base) != "tree" || len(snap.Untracked) != 0 || len(snap.Fingerprints) != 0 {
				t.Fatalf("unborn repository did not capture a baseline tree: %+v", snap)
			}
			if got := gitOut(t, dir, "status", "--porcelain=v1"); got != before {
				t.Fatalf("snapshot changed workspace: %q -> %q", before, got)
			}
			write(t, dir, "staged.txt", "after\nsecond line")
			if err := os.Remove(filepath.Join(dir, "removed.txt")); err != nil {
				t.Fatal(err)
			}
			write(t, dir, "binary", "x\x00y")
			write(t, dir, "odd\tname\n.txt", "new\n")
			if err := os.Symlink("missing", filepath.Join(dir, "link")); err != nil {
				t.Fatal(err)
			}
			subdir := filepath.Join(dir, "nested")
			if err := os.Mkdir(subdir, 0700); err != nil {
				t.Fatal(err)
			}
			write(t, subdir, "child", "child\n")
			if commit {
				git(t, dir, "add", "-A")
				git(t, dir, "commit", "-qm", "first commit")
			}
			before = gitOut(t, dir, "status", "--porcelain=v1")
			indexPath := filepath.Join(dir, ".git", "index")
			index, err := os.ReadFile(indexPath)
			if err != nil {
				t.Fatal(err)
			}
			d, err := DiffSince(subdir, snap)
			if err != nil {
				t.Fatal(err)
			}
			want := Diff{Files: []FileDelta{
				{Path: "binary", New: true},
				{Path: "link", Added: 1, New: true},
				{Path: "nested/child", Added: 1, New: true},
				{Path: "odd\tname\n.txt", Added: 1, New: true},
				{Path: "removed.txt", Deleted: 1},
				{Path: "staged.txt", Added: 2, Deleted: 1},
			}, Added: 5, Deleted: 2}
			if !reflect.DeepEqual(d, want) {
				t.Fatalf("diff=%+v want=%+v", d, want)
			}
			if got := gitOut(t, dir, "status", "--porcelain=v1"); got != before {
				t.Fatalf("diff changed workspace: %q -> %q", before, got)
			}
			after, err := os.ReadFile(indexPath)
			if err != nil || !reflect.DeepEqual(index, after) {
				t.Fatalf("diff changed index: %v", err)
			}
		})
	}
}
