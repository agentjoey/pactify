package sub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func finishWorktreeResult(t *testing.T, s Store, id string) Result {
	t.Helper()
	r := Result{JobID: id, Status: StatusSucceeded, SessionID: "session-" + id}
	if err := s.Finish(id, &r, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWorktreeReportsCommitsAndNewBranchesAcrossResume(t *testing.T) {
	s, st := worktreeFixture(t)
	w := st.Worktree.Path
	// These mutations are confined to the disposable fixture repository.
	for i := 0; i < 3; i++ {
		write(t, w, "a.txt", strings.Repeat("changed\n", i+1))
		git(t, w, "add", "a.txt")
		git(t, w, "commit", "-qm", "harness commit")
	}
	git(t, w, "branch", "test-build-vcs")
	head := gitOut(t, w, "rev-parse", "HEAD")
	index := gitOut(t, w, "diff", "--cached", "--binary")
	// Reload via a fresh Store, as a restarted worker would.
	s = Store{Root: s.Root}
	r := finishWorktreeResult(t, s, st.ID)
	if !contains(r.Warnings, "harness committed 3 commit(s) in the worktree; changes are still included in the diff") ||
		!contains(r.Warnings, "new branch(es) appeared in the repository while the job ran, likely created by the harness: test-build-vcs (not removed)") {
		t.Fatalf("missing warnings: %q", r.Warnings)
	}
	if len(r.Diff.Files) != 1 || r.Diff.Files[0].Path != "a.txt" {
		t.Fatalf("committed diff lost: %+v", r.Diff)
	}
	if gitOut(t, w, "rev-parse", "HEAD") != head || gitOut(t, w, "diff", "--cached", "--binary") != index || gitOut(t, w, "rev-parse", "refs/heads/test-build-vcs") != head {
		t.Fatal("detection mutated git state")
	}
	for _, round := range []string{"unchanged", "new", "older-resume"} {
		next, err := s.ResumeRun(round, st.Request, false, st.ID)
		if err != nil {
			t.Fatal(err)
		}
		if round == "new" {
			write(t, w, "b.txt", "new round\n")
			git(t, w, "add", "b.txt")
			git(t, w, "commit", "-qm", "next commit")
			git(t, w, "branch", "second-branch")
		}
		r = finishWorktreeResult(t, s, next.ID)
		if round == "new" {
			if !contains(r.Warnings, "harness committed 4 commit(s)") || !contains(r.Warnings, "by the harness: second-branch (not removed)") || contains(r.Warnings, "test-build-vcs") {
				t.Fatalf("new warnings: %q", r.Warnings)
			}
		} else if contains(r.Warnings, "harness committed") || contains(r.Warnings, "new branch(es) appeared") {
			t.Fatalf("repeated warnings: %q", r.Warnings)
		}
	}
}

func TestWorktreeReportsNonDescendantHead(t *testing.T) {
	s, st := worktreeFixture(t)
	unrelated := gitOut(t, st.Worktree.Path, "commit-tree", st.Worktree.BaseCommit+"^{tree}", "-m", "unrelated")
	git(t, st.Worktree.Path, "checkout", "--detach", unrelated)
	r := finishWorktreeResult(t, s, st.ID)
	if !contains(r.Warnings, "harness worktree HEAD moved") || !contains(r.Warnings, "not a descendant") {
		t.Fatalf("warnings: %q", r.Warnings)
	}
	if gitOut(t, st.Worktree.Path, "rev-parse", "HEAD") != unrelated {
		t.Fatal("HEAD reset")
	}
}

func TestWorktreeBranchBaselinePersistedAndLegacySafe(t *testing.T) {
	s, st := worktreeFixture(t)
	data, err := os.ReadFile(filepath.Join(s.Dir(st.ID), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err = json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	var worktree map[string]json.RawMessage
	if err = json.Unmarshal(raw["worktree"], &worktree); err != nil {
		t.Fatal(err)
	}
	var branches []string
	if err = json.Unmarshal(worktree["initialBranches"], &branches); err != nil || len(branches) == 0 {
		t.Fatalf("branch baseline missing: %s (%v)", raw["worktree"], err)
	}
	// A legacy job has no branch snapshot. Do not label existing branches as new.
	delete(worktree, "initialBranches")
	raw["worktree"], _ = json.Marshal(worktree)
	data, _ = json.Marshal(raw)
	if err = os.WriteFile(filepath.Join(s.Dir(st.ID), "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	r := finishWorktreeResult(t, s, st.ID)
	if r.Status != StatusSucceeded || contains(r.Warnings, "new branch(es) appeared") {
		t.Fatalf("legacy result: %+v", r)
	}
}

func TestWorktreeEmptyBranchBaselineAndRecovery(t *testing.T) {
	root := repo(t)
	branch := gitOut(t, root, "branch", "--show-current")
	git(t, root, "checkout", "--detach")
	git(t, root, "branch", "-D", branch)
	s := Store{Root: t.TempDir()}
	st, err := s.CreateRun("empty-refs", Request{Harness: "fake", Access: "write", Cwd: root}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = runGit(root, "worktree", "remove", "--force", st.Worktree.Path) })
	st, err = s.Load(st.ID)
	if err != nil || st.Worktree.InitialBranches == nil || len(*st.Worktree.InitialBranches) != 0 {
		t.Fatalf("empty baseline not persisted: %+v %v", st.Worktree, err)
	}
	git(t, st.Worktree.Path, "branch", "new-branch")
	st.Status = StatusRunning
	if err = s.Save(st); err != nil {
		t.Fatal(err)
	}
	// A dead worker is finalized through Recover rather than Finish.
	if _, err = s.Recover(st.ID); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.ResultPath(st.ID))
	var r Result
	if err != nil || json.Unmarshal(data, &r) != nil || !contains(r.Warnings, "by the harness: new-branch (not removed)") {
		t.Fatalf("recovered result: %s %v", data, err)
	}
}

func TestWorktreeTransferRetainsWarningHistory(t *testing.T) {
	s, st := worktreeFixture(t)
	git(t, st.Worktree.Path, "branch", "first-branch")
	finishWorktreeResult(t, s, st.ID)
	next, err := s.WorktreeFrom("transferred", st.Request, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	git(t, next.Worktree.Path, "branch", "new-branch")
	r := finishWorktreeResult(t, s, next.ID)
	if !contains(r.Warnings, "by the harness: new-branch (not removed)") || contains(r.Warnings, "first-branch") {
		t.Fatalf("transfer warnings: %q", r.Warnings)
	}
}

func TestWorktreePreservesBranchRefNames(t *testing.T) {
	s, st := worktreeFixture(t)
	// Git permits Unicode whitespace in refs even though ASCII spaces are banned.
	branch := "test\u2003branch"
	git(t, st.Worktree.Path, "branch", branch)
	r := finishWorktreeResult(t, s, st.ID)
	if !contains(r.Warnings, "by the harness: "+branch+" (not removed)") {
		t.Fatalf("branch name was changed: %q", r.Warnings)
	}
}
