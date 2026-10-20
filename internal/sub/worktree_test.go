package sub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func worktreeFixture(t *testing.T) (Store, State) {
	t.Helper()
	s := Store{Root: t.TempDir()}
	st, err := s.CreateRun("first", Request{Harness: "fake", Access: "write", Cwd: repo(t), Brief: "task"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = runGit(st.Request.Cwd, "worktree", "remove", "--force", st.Worktree.Path) })
	return s, st
}

func finishWorktree(t *testing.T, s Store, id string, status Status) {
	t.Helper()
	r := Result{JobID: id, Status: status, SessionID: "session-" + id}
	if err := s.Finish(id, &r, "", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeDetachedSubdirectoryAndResume(t *testing.T) {
	s, st := worktreeFixture(t)
	if got := gitOut(t, st.Worktree.Path, "rev-parse", "HEAD"); got != st.Worktree.BaseCommit {
		t.Fatalf("HEAD=%s", got)
	}
	if got := gitOut(t, st.Worktree.Path, "branch", "--show-current"); got != "" {
		t.Fatalf("created branch %s", got)
	}
	finishWorktree(t, s, st.ID, StatusSucceeded)
	for _, isolated := range []bool{false, true} {
		id := "resume-auto"
		if isolated {
			id = "resume-explicit"
		}
		next, err := s.CreateRun(id, st.Request, isolated, true)
		if err != nil {
			t.Fatal(err)
		}
		if next.Worktree.Path != st.Worktree.Path || next.Worktree.BaseCommit != st.Worktree.BaseCommit || next.Request.Resume == "" {
			t.Fatalf("resume=%+v", next)
		}
		// A previous turn cannot discard or apply while this turn is queued.
		for _, action := range []func(string) error{s.DiscardWorktree, s.ApplyWorktree} {
			if err := action(st.ID); err == nil || !strings.Contains(err.Error(), "cancel") {
				t.Fatalf("active child not protected: %v", err)
			}
		}
		if _, err := s.CreateRun("concurrent", st.Request, true, true); err == nil {
			t.Fatal("concurrent resume accepted")
		}
		finishWorktree(t, s, id, StatusFailed)
	}
	if err := s.DiscardWorktree("resume-explicit"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{st.ID, "resume-auto", "resume-explicit"} {
		state, err := s.Load(id)
		if err != nil || !state.Worktree.Discarded {
			t.Fatalf("discard %s: %+v %v", id, state, err)
		}
	}
	root := repo(t)
	child := filepath.Join(root, "nested")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	st, err := s.CreateRun("nested", Request{Cwd: child}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if st.Worktree.Cwd != filepath.Join(st.Worktree.Path, "nested") {
		t.Fatalf("cwd=%s", st.Worktree.Cwd)
	}
	if _, err := os.Stat(st.Worktree.Cwd); err != nil {
		t.Fatal(err)
	}
	finishWorktree(t, s, st.ID, StatusSucceeded)
	if err := s.DiscardWorktree(st.ID); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeApplyRejectsRelatedChangesAtomically(t *testing.T) {
	for _, kind := range []string{"unstaged", "staged", "staged-restored", "new-collision", "ignored-collision", "mode", "later-commit"} {
		t.Run(kind, func(t *testing.T) {
			s, st := worktreeFixture(t)
			write(t, st.Worktree.Path, "a.txt", "callee\n")
			write(t, st.Worktree.Path, "new.txt", "callee new\n")
			finishWorktree(t, s, st.ID, StatusSucceeded)
			switch kind {
			case "new-collision", "ignored-collision":
				write(t, st.Request.Cwd, "new.txt", "user\n")
				if kind == "ignored-collision" {
					write(t, st.Request.Cwd, ".gitignore", "new.txt\n")
				}
			case "mode":
				git(t, st.Request.Cwd, "config", "core.filemode", "false")
				if err := os.Chmod(filepath.Join(st.Request.Cwd, "a.txt"), 0700); err != nil {
					t.Fatal(err)
				}
			default:
				write(t, st.Request.Cwd, "a.txt", "user\n")
				if kind != "unstaged" {
					git(t, st.Request.Cwd, "add", "a.txt")
				}
				if kind == "staged-restored" {
					write(t, st.Request.Cwd, "a.txt", "one\n")
				}
				if kind == "later-commit" {
					git(t, st.Request.Cwd, "commit", "-qm", "user")
				}
			}
			before := gitOut(t, st.Request.Cwd, "status", "--porcelain=v1", "--untracked-files=all")
			beforeA, _ := os.ReadFile(filepath.Join(st.Request.Cwd, "a.txt"))
			if err := s.ApplyWorktree(st.ID); err == nil {
				t.Fatal("apply accepted user changes")
			}
			if after := gitOut(t, st.Request.Cwd, "status", "--porcelain=v1", "--untracked-files=all"); after != before {
				t.Fatalf("partial apply: %q -> %q", before, after)
			}
			if afterA, _ := os.ReadFile(filepath.Join(st.Request.Cwd, "a.txt")); string(afterA) != string(beforeA) {
				t.Fatalf("clobbered a.txt: %q", afterA)
			}
		})
	}
}

func TestWorktreeApplyCompletePatchPreservesIndexes(t *testing.T) {
	s, st := worktreeFixture(t)
	w := st.Worktree.Path
	write(t, w, "a.txt", "changed\n")
	if err := os.Remove(filepath.Join(w, "b.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, w, "new\tfile\n.txt", "new\n")
	write(t, w, "binary", "\x00\x01\x02")
	write(t, w, "script", "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(w, "script"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(w, "link")); err != nil {
		t.Fatal(err)
	}
	git(t, w, "add", "a.txt")
	write(t, st.Request.Cwd, "unrelated.txt", "keep\n")
	git(t, st.Request.Cwd, "add", "unrelated.txt")
	beforeSourceIndex := gitOut(t, st.Request.Cwd, "diff", "--cached", "--binary")
	beforeCalleeIndex := gitOut(t, w, "diff", "--cached", "--binary")
	head := gitOut(t, st.Request.Cwd, "rev-parse", "HEAD")
	finishWorktree(t, s, st.ID, StatusSucceeded)
	if err := s.ApplyWorktree(st.ID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "new\tfile\n.txt", "binary", "script"} {
		want, _ := os.ReadFile(filepath.Join(w, name))
		got, err := os.ReadFile(filepath.Join(st.Request.Cwd, name))
		if err != nil || string(got) != string(want) {
			t.Fatalf("%q: %q %v", name, got, err)
		}
	}
	if target, err := os.Readlink(filepath.Join(st.Request.Cwd, "link")); err != nil || target != "a.txt" {
		t.Fatalf("link=%s %v", target, err)
	}
	if info, err := os.Stat(filepath.Join(st.Request.Cwd, "script")); err != nil || info.Mode()&0100 == 0 {
		t.Fatalf("mode=%v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(st.Request.Cwd, "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("delete: %v", err)
	}
	if got := gitOut(t, st.Request.Cwd, "diff", "--cached", "--binary"); got != beforeSourceIndex {
		t.Fatal("source index changed")
	}
	if got := gitOut(t, w, "diff", "--cached", "--binary"); got != beforeCalleeIndex {
		t.Fatal("callee index changed")
	}
	if got := gitOut(t, st.Request.Cwd, "rev-parse", "HEAD"); got != head {
		t.Fatal("apply committed")
	}
}

func TestWorktreeApplyIncludesIgnoredCommittedAddition(t *testing.T) {
	s, st := worktreeFixture(t)
	write(t, st.Worktree.Path, ".gitignore", "committed.txt\n")
	write(t, st.Worktree.Path, "committed.txt", "committed\n")
	git(t, st.Worktree.Path, "add", ".gitignore")
	git(t, st.Worktree.Path, "add", "-f", "committed.txt")
	git(t, st.Worktree.Path, "commit", "-qm", "callee commit")
	finishWorktree(t, s, st.ID, StatusSucceeded)
	if err := s.ApplyWorktree(st.ID); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(st.Request.Cwd, "committed.txt")); err != nil || string(b) != "committed\n" {
		t.Fatalf("lost committed addition: %q %v", b, err)
	}
}

func TestWorktreeResumeMissingDirectoryRejectedBeforeJobCreation(t *testing.T) {
	s, st := worktreeFixture(t)
	finishWorktree(t, s, st.ID, StatusSucceeded)
	git(t, st.Request.Cwd, "worktree", "remove", "--force", st.Worktree.Path)
	if _, err := s.CreateRun("missing", st.Request, true, true); err == nil {
		t.Fatal("resumed absent worktree")
	}
	if _, err := s.Load("missing"); !os.IsNotExist(err) {
		t.Fatalf("left phantom job: %v", err)
	}
}

func TestRunWorktreeUsesIsolatedCwdForResumeAndConceal(t *testing.T) {
	s, st := worktreeFixture(t)
	for _, id := range []string{st.ID, "second"} {
		if id != st.ID {
			var err error
			st, err = s.CreateRun(id, st.Request, false, true)
			if err != nil {
				t.Fatal(err)
			}
		}
		sess := &fakeSession{events: make(chan cockpit.Event, 4), approvals: make(chan cockpit.ApprovalRequest)}
		backend := &fakeBackend{sess: sess}
		sess.script = func(ss *fakeSession) {
			write(t, backend.gotOpts.RepoDir, "continued.txt", id+"\n")
			ss.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
		}
		h := newFakeHarness(backend)
		var concealed ConcealContext
		h.Conceal = func(c ConcealContext) error { concealed = c; return nil }
		r, err := RunJob(context.Background(), s, id, h, WorkerConfig{})
		if err != nil || r.Status != StatusSucceeded {
			t.Fatalf("run=%+v %v", r, err)
		}
		if backend.gotOpts.RepoDir != st.Worktree.Cwd || concealed.Cwd != st.Worktree.Cwd || !sess.closed.Load() {
			t.Fatalf("opts=%+v concealed=%+v", backend.gotOpts, concealed)
		}
		if id == "second" && backend.resumed != "th-fake" {
			t.Fatalf("resume=%s", backend.resumed)
		}
		if len(r.Diff.Files) != 1 || r.Diff.Files[0].Path != "continued.txt" {
			t.Fatalf("diff=%+v", r.Diff)
		}
	}
}

func TestWorktreeDiscardRemovesMissingDirectoryRegistration(t *testing.T) {
	s, st := worktreeFixture(t)
	finishWorktree(t, s, st.ID, StatusSucceeded)
	if err := os.RemoveAll(st.Worktree.Path); err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardWorktree(st.ID); err != nil {
		t.Fatal(err)
	}
	if list := gitOut(t, st.Request.Cwd, "worktree", "list", "--porcelain"); strings.Contains(list, st.Worktree.Path) {
		t.Fatalf("registration remains: %s", list)
	}
}

func TestWorktreeSummaryUsesFinalTreeDespiteIndexDeletion(t *testing.T) {
	s, st := worktreeFixture(t)
	git(t, st.Worktree.Path, "rm", "a.txt")
	write(t, st.Worktree.Path, "a.txt", "one\nnew line\n")
	finishWorktree(t, s, st.ID, StatusSucceeded)
	data, err := os.ReadFile(s.ResultPath(st.ID))
	var result Result
	if err != nil || json.Unmarshal(data, &result) != nil {
		t.Fatalf("result=%s err=%v", data, err)
	}
	if len(result.Diff.Files) != 1 || result.Diff.Added != 1 || result.Diff.Deleted != 0 {
		t.Fatalf("diff counts staged deletion instead of final tree: %+v", result.Diff)
	}
}

func TestWorktreeResumeSkipsStartupFailureWithoutSession(t *testing.T) {
	s, st := worktreeFixture(t)
	finishWorktree(t, s, st.ID, StatusSucceeded)
	second, err := s.CreateRun("second", st.Request, true, true)
	if err != nil {
		t.Fatal(err)
	}
	r := Result{JobID: second.ID, Status: StatusFailed}
	if err := s.Finish(second.ID, &r, "startup failed", time.Now()); err != nil {
		t.Fatal(err)
	}
	third, err := s.CreateRun("third", st.Request, false, true)
	if err != nil || third.Request.Resume != "session-first" {
		t.Fatalf("lost resumable session: %+v %v", third, err)
	}
	finishWorktree(t, s, "third", StatusCanceled)
}

func TestWorktreeRecoverKeepsReviewableChanges(t *testing.T) {
	s, st := worktreeFixture(t)
	write(t, st.Worktree.Path, "out.txt", "recover me\n")
	if err := s.WithState(context.Background(), st.ID, func(current *State) error {
		current.CreatedAt = time.Now().Add(-time.Minute)
		current.UpdatedAt = current.CreatedAt
		return s.Save(*current)
	}); err != nil {
		t.Fatal(err)
	}
	st, err := s.Recover(st.ID)
	if err != nil || st.Status != StatusFailed {
		t.Fatalf("recover=%+v %v", st, err)
	}
	data, err := os.ReadFile(s.ResultPath(st.ID))
	var result Result
	if err != nil || json.Unmarshal(data, &result) != nil || result.WorktreePath != st.Worktree.Path || len(result.Diff.Files) != 1 {
		t.Fatalf("result=%s %v", data, err)
	}
	if err := s.ApplyWorktree(st.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardWorktree(st.ID); err != nil {
		t.Fatal(err)
	}
}

// AJ-770: on a case-insensitive volume the cwd may be spelled differently
// from git's root (~/agentworks vs ~/AgentWorks) and still name the same directory.
func TestWorktreeCwdSpelledInDifferentCase(t *testing.T) {
	root := repo(t)
	child := filepath.Join(root, "nested", "Dir é")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	cwd := strings.ToUpper(child)
	want, _ := os.Stat(child)
	if got, err := os.Stat(cwd); err != nil || !os.SameFile(want, got) {
		t.Skip("filesystem is case-sensitive")
	}
	s := Store{Root: t.TempDir()}
	st, err := s.CreateRun("case", Request{Cwd: cwd}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = runGit(root, "worktree", "remove", "--force", st.Worktree.Path) }()
	if st.Worktree.Cwd != filepath.Join(st.Worktree.Path, "nested", "Dir é") {
		t.Fatalf("cwd=%s", st.Worktree.Cwd)
	}
	if info, err := os.Stat(st.Worktree.Cwd); err != nil || !info.IsDir() {
		t.Fatalf("isolated cwd missing: %v", err)
	}
}

func TestWorktreeCwdCannotFollowHeadSymlinkOutsideIsolation(t *testing.T) {
	root := repo(t)
	outside := t.TempDir()
	child := filepath.Join(root, "nested")
	if err := os.Symlink(outside, child); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "nested")
	git(t, root, "commit", "-qm", "directory link")
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	s := Store{Root: t.TempDir()}
	st, err := s.CreateRun("escape", Request{Cwd: child}, true, false)
	defer func() { _, _ = runGit(root, "worktree", "remove", "--force", filepath.Join(s.Dir("escape"), "wt")) }()
	if err == nil {
		t.Fatalf("isolated cwd points outside worktree: %+v", st.Worktree)
	}
}

func TestWorktreeSetupHoldsLifecycleLock(t *testing.T) {
	root := repo(t)
	s := Store{Root: t.TempDir()}
	hookDir := filepath.Join(root, ".git", "hooks")
	marker := filepath.Join(t.TempDir(), "checkout-started")
	releasePath := filepath.Join(t.TempDir(), "release")
	// Controlled checkout hook keeps git worktree add in progress while a
	// separate status/recovery caller attempts to acquire the lifecycle lock.
	hook := "#!/bin/sh\ntouch '" + marker + "'\nwhile [ ! -e '" + releasePath + "' ]; do sleep 0.01; done\n"
	if err := os.WriteFile(filepath.Join(hookDir, "post-checkout"), []byte(hook), 0700); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(func() { _ = os.WriteFile(releasePath, nil, 0600) }) }
	defer release()
	created := make(chan error, 1)
	go func() { _, err := s.CreateRun("setup", Request{Cwd: root}, true, false); created <- err }()
	defer func() {
		release()
		<-created
		_, _ = runGit(root, "worktree", "remove", "--force", filepath.Join(s.Dir("setup"), "wt"))
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("checkout hook did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	recovered := make(chan error, 1)
	go func() { _, err := s.Recover("setup"); recovered <- err }()
	select {
	case err := <-recovered:
		t.Fatalf("Recover crossed an in-progress worktree checkout: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	if err := <-recovered; err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeStartupGraceBeginsAfterPreparation(t *testing.T) {
	s, st := worktreeFixture(t)
	if err := s.WithState(context.Background(), st.ID, func(current *State) error {
		// Slow checkout started long ago, but preparation has just completed.
		current.CreatedAt = time.Now().Add(-time.Minute)
		return s.Save(*current)
	}); err != nil {
		t.Fatal(err)
	}
	st, err := s.Recover(st.ID)
	if err != nil || st.Status != StatusQueued {
		t.Fatalf("slow checkout lost startup grace: %+v %v", st, err)
	}
}
