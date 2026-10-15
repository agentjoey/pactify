package sub

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestDiffSinceChangedPreexistingUntracked(t *testing.T) {
	for _, staged := range []bool{false, true} {
		dir := repo(t)
		write(t, dir, "continued.txt", "first turn\n")
		write(t, dir, "user.txt", "untouched\n")
		snap, err := TakeSnapshot(dir)
		if err != nil {
			t.Fatal(err)
		}
		write(t, dir, "continued.txt", "second turn\nmore\n")
		if staged {
			git(t, dir, "add", "continued.txt", "user.txt")
		}
		d, err := DiffSince(dir, snap)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Files) != 1 || d.Files[0].Path != "continued.txt" || d.Added != 2 {
			t.Fatalf("staged=%v diff=%+v", staged, d)
		}
	}
}

func TestSnapshotPreservesPreexistingNestedRepoAndSpecialFile(t *testing.T) {
	dir := repo(t)
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, nested, "init", "-q")
	write(t, nested, "a.txt", "nested content\n")
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	snap, err := TakeSnapshot(dir)
	if err != nil {
		t.Fatalf("preexisting special paths broke snapshot: %v", err)
	}
	d, err := DiffSince(dir, snap)
	if err != nil || len(d.Files) != 0 {
		t.Fatalf("preexisting paths counted: %+v %v", d, err)
	}
}

func TestDiffSinceUntrackedSymlinkFingerprint(t *testing.T) {
	dir := repo(t)
	if err := os.Symlink("a.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	snap, err := TakeSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("b.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	d, err := DiffSince(dir, snap)
	if err != nil || len(d.Files) != 1 || d.Files[0].Path != "link" {
		t.Fatalf("diff=%+v err=%v", d, err)
	}
}
