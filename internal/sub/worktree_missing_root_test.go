package sub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// missingRootFixture starts a worktree job from a linked worktree of the
// repository (as a job started inside another job's worktree does), then
// removes that linked worktree so the job's OriginalRoot no longer exists.
func missingRootFixture(t *testing.T) (Store, State, string) {
	t.Helper()
	main := repo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	gitOut(t, main, "worktree", "add", "--detach", linked)
	s := Store{Root: t.TempDir()}
	st, err := s.CreateRun("job", Request{Harness: "fake", Access: "write", Cwd: linked, Brief: "task"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = runGit(main, "worktree", "remove", "--force", "--force", st.Worktree.Path) })
	finishWorktree(t, s, st.ID, StatusFailed)
	gitOut(t, main, "worktree", "remove", "--force", linked)
	if _, err := os.Stat(st.Worktree.OriginalRoot); !os.IsNotExist(err) {
		t.Fatalf("original root still exists: %v", err)
	}
	return s, st, main
}

func resolved(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func assertUnregistered(t *testing.T, main, path string) {
	t.Helper()
	parent := resolved(t, filepath.Dir(path))
	want := "worktree " + filepath.Join(parent, filepath.Base(path))
	for _, line := range strings.Split(gitOut(t, main, "worktree", "list", "--porcelain"), "\n") {
		if line == want {
			t.Fatalf("registration remains in the main repository: %s", line)
		}
	}
}

func assertDiscarded(t *testing.T, s Store, id string) {
	t.Helper()
	st, err := s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Worktree.Discarded {
		t.Fatal("job not marked discarded")
	}
	if got := s.Actions(st); len(got) != 0 {
		t.Fatalf("actions after discard: %v", got)
	}
	if _, err := os.Lstat(st.Worktree.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree directory remains: %v", err)
	}
}

func TestDiscardWhenOriginalRootIsGone(t *testing.T) {
	s, st, main := missingRootFixture(t)
	notes, err := s.DiscardWorktreeNotes(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertDiscarded(t, s, st.ID)
	assertUnregistered(t, main, st.Worktree.Path)
	if len(notes) == 0 || !strings.Contains(strings.Join(notes, "\n"), "used repository "+resolved(t, main)) {
		t.Fatalf("notes do not name the repository used: %v", notes)
	}
}

func TestDiscardWhenOriginalRootAndWorktreeAreGone(t *testing.T) {
	s, st, _ := missingRootFixture(t)
	if err := os.RemoveAll(st.Worktree.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DiscardWorktreeNotes(st.ID); err != nil {
		t.Fatal(err)
	}
	assertDiscarded(t, s, st.ID)
}

func TestDiscardWhenRepositoryIsGone(t *testing.T) {
	s, st, main := missingRootFixture(t)
	if err := os.RemoveAll(main); err != nil {
		t.Fatal(err)
	}
	notes, err := s.DiscardWorktreeNotes(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertDiscarded(t, s, st.ID)
	if !strings.Contains(strings.Join(notes, "\n"), "worktree prune") {
		t.Fatalf("notes do not explain leftover git metadata: %v", notes)
	}
}

// Without a repository, only the job's own worktree directory may be removed.
func TestDiscardWithoutRepositoryKeepsForeignPath(t *testing.T) {
	s, st, main := missingRootFixture(t)
	if err := os.RemoveAll(main); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "foreign")
	if err := os.MkdirAll(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.WithState(t.Context(), st.ID, func(current *State) error {
		current.Worktree.Path = foreign
		return s.Save(*current)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DiscardWorktreeNotes(st.ID); err == nil || !strings.Contains(err.Error(), "outside the job directory") {
		t.Fatalf("discard of a foreign path: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("foreign path was touched: %v", err)
	}
	if st, _ := s.Load(st.ID); st.Worktree.Discarded {
		t.Fatal("refused discard marked the job discarded")
	}
}

// A symlinked wt must not let the git-less removal reach its target.
func TestDiscardWithoutRepositoryRejectsSymlinkedWorktree(t *testing.T) {
	s, st, main := missingRootFixture(t)
	if err := os.RemoveAll(main); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(st.Worktree.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, st.Worktree.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DiscardWorktreeNotes(st.ID); err == nil {
		t.Fatal("discard followed a symlinked worktree")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target was touched: %v", err)
	}
}

// A worktree that lost its .git file inside an enclosing repository must not be
// attributed to that repository.
func TestDiscardIgnoresEnclosingRepository(t *testing.T) {
	main := repo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	gitOut(t, main, "worktree", "add", "--detach", linked)
	enclosing := repo(t)
	s := Store{Root: filepath.Join(enclosing, "store")}
	st, err := s.CreateRun("job", Request{Harness: "fake", Access: "write", Cwd: linked, Brief: "task"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	finishWorktree(t, s, st.ID, StatusFailed)
	gitOut(t, main, "worktree", "remove", "--force", linked)
	if err := os.Remove(filepath.Join(st.Worktree.Path, ".git")); err != nil {
		t.Fatal(err)
	}
	notes, err := s.DiscardWorktreeNotes(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertDiscarded(t, s, st.ID)
	if strings.Contains(strings.Join(notes, "\n"), "used repository") {
		t.Fatalf("attributed to the enclosing repository: %v", notes)
	}
	gitOut(t, main, "worktree", "prune")
}

// The reported case: job B started inside job A's worktree, and A was
// discarded first.
func TestDiscardNestedJobAfterParent(t *testing.T) {
	main := repo(t)
	s := Store{Root: t.TempDir()}
	parent, err := s.CreateRun("parent", Request{Harness: "fake", Access: "write", Cwd: main, Brief: "task"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateRun("child", Request{Harness: "fake", Access: "write", Cwd: parent.Worktree.Path, Brief: "task"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	finishWorktree(t, s, parent.ID, StatusSucceeded)
	finishWorktree(t, s, child.ID, StatusFailed)
	if err := s.DiscardWorktree(parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DiscardWorktreeNotes(child.ID); err != nil {
		t.Fatal(err)
	}
	assertDiscarded(t, s, child.ID)
	assertUnregistered(t, main, child.Worktree.Path)
}

func TestApplyWhenOriginalRootIsGone(t *testing.T) {
	s, st, _ := missingRootFixture(t)
	if err := os.WriteFile(filepath.Join(st.Worktree.Path, "new.txt"), []byte("x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := s.ApplyWorktree(st.ID)
	if err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("apply error = %v", err)
	}
}
