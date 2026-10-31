package sub

import (
	"slices"
	"testing"
	"time"
)

func TestDismissActionEligibility(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    Status
		warnings  []string
		worktree  *Worktree
		dismissed bool
		want      bool
	}{
		{name: "failed", status: StatusFailed, want: true},
		{name: "warnings", status: StatusSucceeded, warnings: []string{"review result"}, want: true},
		{name: "failed and warnings", status: StatusFailed, warnings: []string{"review result"}, want: true},
		{name: "success", status: StatusSucceeded},
		{name: "informational", status: StatusSucceeded, warnings: []string{partialUsageWarning}},
		{name: "canceled warnings", status: StatusCanceled, warnings: []string{"review result"}, want: true},
		{name: "running", status: StatusRunning, warnings: []string{"review result"}},
		{name: "queued", status: StatusQueued},
		{name: "dismissed", status: StatusFailed, dismissed: true},
		{name: "pending failed", status: StatusFailed, worktree: &Worktree{}, want: true},
		{name: "pending warnings", status: StatusSucceeded, warnings: []string{"review result"}, worktree: &Worktree{}, want: true},
		{name: "pending clean", status: StatusSucceeded, worktree: &Worktree{}, want: true},
		{name: "applied", status: StatusFailed, worktree: &Worktree{Applied: true}},
		{name: "discarded", status: StatusFailed, worktree: &Worktree{Discarded: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Store{Root: t.TempDir()}
			st, err := s.Create("job", Request{})
			if err != nil {
				t.Fatal(err)
			}
			st.Status, st.Worktree = tc.status, tc.worktree
			if tc.dismissed {
				st.DismissedAt = time.Now()
			}
			if err := s.Save(st); err != nil {
				t.Fatal(err)
			}
			index := s.NewSummaryIndex([]State{st})
			index.results[st.ID] = Result{Warnings: tc.warnings}
			if got := slices.Contains(index.Actions(st), "dismiss"); got != tc.want {
				t.Errorf("dismiss = %v, want %v", got, tc.want)
			}
			if got := index.CheckAction(st, "dismiss") == nil; got != tc.want {
				t.Errorf("validation = %v, want %v", got, tc.want)
			}
		})
	}
}
