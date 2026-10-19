package sub

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestDismissFailedReadOnlyJob(t *testing.T) {
	s := Store{Root: t.TempDir()}
	st, err := s.Create("readonly", Request{Harness: "codex", Access: "read-only"})
	if err != nil {
		t.Fatal(err)
	}
	r := Result{JobID: st.ID, Status: StatusFailed, FailureClass: "auth", Warnings: []string{"review this"}, SessionID: "session", FinalText: "retained answer"}
	if err := s.Finish(st.ID, &r, "failed", time.Now()); err != nil {
		t.Fatal(err)
	}
	st, err = s.Load(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	before := s.Summarize(st, nil, time.Now())
	if len(before.Attention) != 2 || before.Attention[0].Kind != AttentionFailed || before.Attention[1].Kind != AttentionWarnings {
		t.Fatalf("unacknowledged attention: %v", before.Attention)
	}
	if _, ok := summaryJSON(t, s, st, nil)["dismissedAt"]; ok {
		t.Fatal("zero dismissedAt must be omitted")
	}
	resultBefore, err := os.ReadFile(s.ResultPath(st.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.LogPath(st.ID), []byte("retained log\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Dismiss(context.Background(), st.ID); err != nil {
		t.Fatal(err)
	}
	dismissed, err := s.Load(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dismissed.DismissedAt.IsZero() || dismissed.Status != StatusFailed {
		t.Fatalf("dismissed state: %+v", dismissed)
	}
	after := s.Summarize(dismissed, nil, time.Now())
	if len(after.Attention) != 0 || !after.DismissedAt.Equal(dismissed.DismissedAt) {
		t.Fatalf("dismissed summary: %+v", after)
	}
	before.Attention, before.DismissedAt = []Attention{}, dismissed.DismissedAt
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("dismiss changed other summary fields: before=%+v after=%+v", before, after)
	}
	if got := summaryJSON(t, s, dismissed, nil)["dismissedAt"]; got != dismissed.DismissedAt.Format(time.RFC3339Nano) {
		t.Fatalf("dismissedAt JSON: %v", got)
	}
	resultAfter, err := os.ReadFile(s.ResultPath(st.ID))
	if err != nil || !bytes.Equal(resultBefore, resultAfter) {
		t.Fatalf("result changed: %s, %v", resultAfter, err)
	}
	if log, err := os.ReadFile(s.LogPath(st.ID)); err != nil || string(log) != "retained log\n" {
		t.Fatalf("log changed: %q, %v", log, err)
	}
	if err := s.Dismiss(context.Background(), st.ID); err != nil {
		t.Fatal(err)
	}
	again, err := s.Load(st.ID)
	if err != nil || !reflect.DeepEqual(dismissed, again) {
		t.Fatalf("dismiss must be idempotent: %+v, %v", again, err)
	}
}

func TestDismissGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status Status
		wt     *Worktree
		want   string
	}{
		{"queued", StatusQueued, nil, "job guarded is still queued; wait for it or cancel it first"},
		{"running", StatusRunning, nil, "job guarded is still running; wait for it or cancel it first"},
		{"pending", StatusFailed, &Worktree{}, "job guarded has a pending worktree; apply or discard it first"},
		{"applied", StatusSucceeded, &Worktree{Applied: true}, ""},
		{"discarded", StatusCanceled, &Worktree{Discarded: true}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Store{Root: t.TempDir()}
			st, err := s.Create("guarded", Request{})
			if err != nil {
				t.Fatal(err)
			}
			st.Status, st.Worktree = tc.status, tc.wt
			if err := s.Save(st); err != nil {
				t.Fatal(err)
			}
			err = s.Dismiss(context.Background(), st.ID)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil || err.Error() != tc.want {
					t.Fatalf("got %v, want %q", err, tc.want)
				}
				after, err := s.Load(st.ID)
				if err != nil || !reflect.DeepEqual(st, after) {
					t.Fatalf("rejected dismiss changed state: %+v, %v", after, err)
				}
			}
		})
	}
	s := Store{Root: t.TempDir()}
	if err := s.Dismiss(context.Background(), "unknown"); !os.IsNotExist(err) {
		t.Fatalf("unknown job: %v", err)
	}
}
