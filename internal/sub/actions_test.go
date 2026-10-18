package sub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestJobActions(t *testing.T) {
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	s, st := worktreeFixture(t)
	if got := s.Actions(st); !reflect.DeepEqual(got, []string{"cancel"}) {
		t.Fatal(got)
	}
	finishWorktree(t, s, st.ID, StatusSucceeded)
	st, _ = s.Load(st.ID)
	if got := s.Actions(st); !reflect.DeepEqual(got, []string{"resume", "apply", "discard"}) {
		t.Fatal(got)
	}
	summary := s.Summarize(st, nil, time.Now())
	if !reflect.DeepEqual(summary.Actions, s.Actions(st)) {
		t.Fatal(summary)
	}
	if err := s.CheckAction(st, "cancel"); err == nil {
		t.Fatal("terminal cancel allowed")
	}
	next, err := s.ResumeRun("next", st.Request, false, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.Request.Model != st.Request.Model || next.Request.Effort != st.Request.Effort || next.Worktree.Path != st.Worktree.Path {
		t.Fatal(next)
	}
	if got := s.Actions(st); len(got) != 0 {
		t.Fatal("shared worktree busy", got)
	}
	if _, err := s.CancelJob(context.Background(), next.ID); err != nil {
		t.Fatal(err)
	}
	st, _ = s.Load(st.ID)
	if err := s.ApplyWorktree(st.ID); err != nil {
		t.Fatal(err)
	}
	st, _ = s.Load(st.ID)
	if got := s.Actions(st); !reflect.DeepEqual(got, []string{"discard"}) {
		t.Fatal(got)
	}
	if err := s.DiscardWorktree(st.ID); err != nil {
		t.Fatal(err)
	}
	st, _ = s.Load(st.ID)
	if got := s.Actions(st); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestActionsKeepMissingWorktreeCleanup(t *testing.T) {
	s, st := worktreeFixture(t)
	finishWorktree(t, s, st.ID, StatusSucceeded)
	st, _ = s.Load(st.ID)
	if err := os.RemoveAll(st.Worktree.Path); err != nil {
		t.Fatal(err)
	}
	if got := s.Actions(st); !reflect.DeepEqual(got, []string{"discard"}) {
		t.Fatal(got)
	}
	if err := s.DiscardWorktree(st.ID); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRechecksAppliedState(t *testing.T) {
	s, st := worktreeFixture(t)
	finishWorktree(t, s, st.ID, StatusSucceeded)
	if err := s.ApplyWorktree(st.ID); err != nil {
		t.Fatal(err)
	}
	// Empty patches must also reject a second apply, under the worktree lock.
	var conflict *ActionConflict
	if err := s.ApplyWorktree(st.ID); !errors.As(err, &conflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestCancelActionsFollowFallback(t *testing.T) {
	s := Store{Root: t.TempDir()}
	old, err := s.Create("old", Request{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("current", Request{}); err != nil {
		t.Fatal(err)
	}
	old.Status = StatusFailed
	old.FallbackTo = "current"
	if err := s.Save(old); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckAction(old, "cancel"); err != nil {
		t.Fatal(err)
	}
	if got := s.Actions(old); !reflect.DeepEqual(got, []string{"cancel"}) {
		t.Fatal(got)
	}
}

func TestActionsApplyWhenJobRemovedItsCwd(t *testing.T) {
	s, st := worktreeFixture(t)
	// Applying a deletion needs the worktree root, even if the task removed its
	// original working subdirectory. Only resume requires that cwd to exist.
	st.Worktree.Cwd = filepath.Join(st.Worktree.Path, "removed-subdirectory")
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	finishWorktree(t, s, st.ID, StatusSucceeded)
	st, _ = s.Load(st.ID)
	if got := s.Actions(st); !reflect.DeepEqual(got, []string{"apply", "discard"}) {
		t.Fatal(got)
	}
	if err := s.ApplyWorktree(st.ID); err != nil {
		t.Fatal(err)
	}
}
