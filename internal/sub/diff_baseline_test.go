package sub

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestUnbornBaselineExcludesPreexistingFiles(t *testing.T) {
	for _, after := range []string{"untracked", "staged", "committed"} {
		t.Run(after, func(t *testing.T) {
			dir := unbornRepo(t)
			write(t, dir, "pre.txt", "before\n")
			write(t, dir, ".gitignore", "ignored\nforced.txt\n")
			write(t, dir, "ignored", "ignored\n")
			write(t, dir, "forced.txt", "tracked despite ignore\n")
			git(t, dir, "add", "-f", "forced.txt")
			indexPath := filepath.Join(dir, ".git", "index")
			index, err := os.ReadFile(indexPath)
			if err != nil {
				t.Fatal(err)
			}
			snap, err := TakeSnapshot(dir)
			if err != nil {
				t.Fatal(err)
			}
			if snap.Base == "" || gitOut(t, dir, "cat-file", "-t", snap.Base) != "tree" {
				t.Fatalf("missing baseline tree: %+v", snap)
			}
			if got := gitOut(t, dir, "show", snap.Base+":pre.txt"); got != "before" {
				t.Fatalf("baseline content=%q", got)
			}
			if got, err := os.ReadFile(indexPath); err != nil || !bytes.Equal(index, got) {
				t.Fatalf("snapshot changed user index: %v", err)
			}
			write(t, dir, "hello.txt", "hello\n")
			if after != "untracked" {
				git(t, dir, "add", "-A")
			}
			if after == "committed" {
				git(t, dir, "commit", "-qm", "first")
			}
			index, err = os.ReadFile(indexPath)
			if err != nil {
				t.Fatal(err)
			}
			d, err := DiffSince(dir, snap)
			want := Diff{Files: []FileDelta{{Path: "hello.txt", Added: 1, New: true}}, Added: 1}
			if err != nil || !reflect.DeepEqual(d, want) {
				t.Fatalf("diff=%+v want=%+v err=%v", d, want, err)
			}
			if got, err := os.ReadFile(indexPath); err != nil || !bytes.Equal(index, got) {
				t.Fatalf("diff changed user index: %v", err)
			}
		})
	}
}

func TestUnbornBaselineCountsOnlyJobEdits(t *testing.T) {
	dir := unbornRepo(t)
	write(t, dir, "edit.txt", "one\n")
	write(t, dir, "delete.txt", "gone\n")
	write(t, dir, "staged.txt", "staged\n")
	git(t, dir, "add", "staged.txt")
	write(t, dir, "staged.txt", "staged\nunstaged\n")
	snap, err := TakeSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "edit.txt", "one\ntwo\n")
	write(t, dir, "staged.txt", "staged\nunstaged\njob\n")
	if err := os.Remove(filepath.Join(dir, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	d, err := DiffSince(dir, snap)
	want := Diff{Files: []FileDelta{{Path: "delete.txt", Deleted: 1}, {Path: "edit.txt", Added: 1}, {Path: "staged.txt", Added: 1}}, Added: 2, Deleted: 1}
	if err != nil || !reflect.DeepEqual(d, want) {
		t.Fatalf("diff=%+v want=%+v err=%v", d, want, err)
	}
}

func TestUnbornBaselineWithoutUserIndex(t *testing.T) {
	dir := unbornRepo(t)
	write(t, dir, "pre.txt", "before\n")
	snap, err := TakeSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "hello.txt", "hello\n")
	d, err := DiffSince(dir, snap)
	if err != nil || len(d.Files) != 1 || d.Files[0].Path != "hello.txt" || d.Added != 1 {
		t.Fatalf("diff=%+v err=%v", d, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "index")); !os.IsNotExist(err) {
		t.Fatalf("created user index: %v", err)
	}
}

func TestUnbornBaselinePreservesExclusions(t *testing.T) {
	dir := unbornRepo(t)
	write(t, dir, ".gitignore", "ignored\nforced.txt\n")
	write(t, dir, "ignored", "ignored\n")
	write(t, dir, "forced.txt", "tracked\n")
	git(t, dir, "add", "-f", "forced.txt")
	write(t, filepath.Join(dir, ".git", "info"), "exclude", "local.tmp\n")
	write(t, dir, "local.tmp", "local ignore\n")
	globalExclude := filepath.Join(t.TempDir(), "exclude")
	if err := os.WriteFile(globalExclude, []byte("global.tmp\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "config", "core.excludesFile", globalExclude)
	write(t, dir, "global.tmp", "global ignore\n")
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, nested, "init", "-q")
	write(t, nested, "file.txt", "nested\n")
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	snap, err := TakeSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "forced.txt", "tracked\njob\n")
	write(t, dir, "hello.txt", "hello\n")
	d, err := DiffSince(dir, snap)
	want := Diff{Files: []FileDelta{{Path: "forced.txt", Added: 1}, {Path: "hello.txt", Added: 1, New: true}}, Added: 2}
	if err != nil || !reflect.DeepEqual(d, want) {
		t.Fatalf("diff=%+v want=%+v err=%v", d, want, err)
	}
}

func TestUnbornTemporaryIndexCleanup(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "git add failed"}[failed], func(t *testing.T) {
			dir := unbornRepo(t)
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			write(t, dir, "pre.txt", "before\n")
			if failed {
				write(t, dir, ".gitattributes", "pre.txt filter=fail\n")
				git(t, dir, "config", "filter.fail.clean", "false")
				git(t, dir, "config", "filter.fail.required", "true")
			}
			snap, err := TakeSnapshot(dir)
			if failed {
				if err == nil || !strings.Contains(err.Error(), "filter") {
					t.Fatalf("snapshot error=%v", err)
				}
			} else {
				if err != nil || snap.Base == "" {
					t.Fatalf("snapshot=%+v err=%v", snap, err)
				}
				if _, err := DiffSince(dir, snap); err != nil {
					t.Fatal(err)
				}
			}
			entries, err := os.ReadDir(tmp)
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary index leaked: %v err=%v", entries, err)
			}
			if _, err := os.Stat(filepath.Join(dir, ".git", "index")); !os.IsNotExist(err) {
				t.Fatalf("created user index: %v", err)
			}
		})
	}
}

func TestWorktreeNeedsCommitMessageEnglish(t *testing.T) {
	store := Store{Root: t.TempDir()}
	_, err := store.CreateRun("unborn", Request{Cwd: unbornRepo(t)}, true, false)
	const want = "--worktree needs a base commit; commit once first (the repository has no commits yet)"
	if !errors.Is(err, ErrWorktreeNeedsCommit) || err.Error() != want {
		t.Fatalf("error=%v want=%q", err, want)
	}
}

func TestRunJobUnbornBaselineExcludesPreexistingFile(t *testing.T) {
	store, session, backend := workerFixture(t, "write")
	st, err := store.Load("j1")
	if err != nil {
		t.Fatal(err)
	}
	st.Request.Cwd = unbornRepo(t)
	write(t, st.Request.Cwd, "pre.txt", "before\n")
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	session.script = func(s *fakeSession) {
		if err := os.WriteFile(filepath.Join(st.Request.Cwd, "hello.txt"), []byte("hello\n"), 0600); err != nil {
			s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: err.Error()}
			return
		}
		s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "DONE"}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(backend), WorkerConfig{})
	want := Diff{Files: []FileDelta{{Path: "hello.txt", Added: 1, New: true}}, Added: 1}
	if err != nil || r.Status != StatusSucceeded || !reflect.DeepEqual(r.Diff, want) {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestRunJobUnbornBaselineUnavailableWarns(t *testing.T) {
	store, session, backend := workerFixture(t, "write")
	st, err := store.Load("j1")
	if err != nil {
		t.Fatal(err)
	}
	st.Request.Cwd = unbornRepo(t)
	write(t, st.Request.Cwd, "pre.txt", "before\n")
	write(t, st.Request.Cwd, ".gitattributes", "pre.txt filter=fail\n")
	git(t, st.Request.Cwd, "config", "filter.fail.clean", "false")
	git(t, st.Request.Cwd, "config", "filter.fail.required", "true")
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	session.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "DONE"}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(backend), WorkerConfig{})
	if err != nil || r.Status != StatusSucceeded || r.FinalText != "DONE" || !contains(r.Warnings, "baseline") || !contains(r.Warnings, "Changes") || len(r.Diff.Files) != 0 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}
