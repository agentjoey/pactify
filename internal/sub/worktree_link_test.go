package sub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func breakLink(t *testing.T, w, kind string) {
	t.Helper()
	link := filepath.Join(w, ".git")
	switch kind {
	case "missing":
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
	case "garbage":
		write(t, w, ".git", "not a gitdir line\n")
	case "dangling":
		write(t, w, ".git", "gitdir: "+filepath.Join(t.TempDir(), "gone")+"\n")
	}
}

func TestWorktreeFinishApplyRepairBrokenLink(t *testing.T) {
	for _, kind := range []string{"missing", "garbage", "dangling"} {
		t.Run(kind, func(t *testing.T) {
			s, st := worktreeFixture(t)
			w := st.Worktree.Path
			write(t, w, "a.txt", "callee\n")
			write(t, w, "keep.txt", "untracked user file\n")
			breakLink(t, w, kind)
			r := finishWorktreeResult(t, s, st.ID)
			if r.Status != StatusSucceeded || len(r.Diff.Files) != 2 {
				t.Fatalf("finish: status=%s diff=%+v warnings=%v", r.Status, r.Diff, r.Warnings)
			}
			breakLink(t, w, kind)
			if err := s.ApplyWorktree(st.ID); err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(filepath.Join(st.Request.Cwd, "a.txt")); string(got) != "callee\n" {
				t.Errorf("a.txt = %q", got)
			}
			if got, _ := os.ReadFile(filepath.Join(w, "keep.txt")); string(got) != "untracked user file\n" {
				t.Errorf("worktree file changed: %q", got)
			}
		})
	}
}

func TestWorktreeDiscardRepairsBrokenLink(t *testing.T) {
	s, st := worktreeFixture(t)
	finishWorktree(t, s, st.ID, StatusSucceeded)
	breakLink(t, st.Worktree.Path, "missing")
	if err := s.DiscardWorktree(st.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(st.Worktree.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree remains: %v", err)
	}
	assertUnregistered(t, st.Request.Cwd, st.Worktree.Path)
}

func TestWorktreeBrokenLinkWithoutRegistrationGivesRecoveryHint(t *testing.T) {
	s, st := worktreeFixture(t)
	w := st.Worktree.Path
	write(t, w, "a.txt", "callee\n")
	finishWorktree(t, s, st.ID, StatusSucceeded)
	breakLink(t, w, "missing")
	// Forget the registration, as git worktree prune does for a lost link.
	git(t, st.Request.Cwd, "worktree", "prune")
	err := s.ApplyWorktree(st.ID)
	if err == nil {
		t.Fatal("apply succeeded without a repository link")
	}
	for _, want := range []string{w, "could not be repaired", "discard the job"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(w, "a.txt")); string(got) != "callee\n" {
		t.Errorf("worktree file changed: %q", got)
	}
}

func TestWorktreeDotGitDirectoryIsNotOverwritten(t *testing.T) {
	s, st := worktreeFixture(t)
	w := st.Worktree.Path
	finishWorktree(t, s, st.ID, StatusSucceeded)
	if err := os.Remove(filepath.Join(w, ".git")); err != nil {
		t.Fatal(err)
	}
	git(t, w, "init", "-q")
	marker := filepath.Join(w, ".git", "marker")
	write(t, filepath.Join(w, ".git"), "marker", "mine\n")
	err := s.ApplyWorktree(st.ID)
	if err == nil || !strings.Contains(err.Error(), w) {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Errorf(".git directory was modified: %v", statErr)
	}
}
