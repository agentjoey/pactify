package sub

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestRunJobUnbornRepository(t *testing.T) {
	for _, access := range []string{"read-only", "write"} {
		t.Run(access, func(t *testing.T) {
			store, session, backend := workerFixture(t, access)
			st, err := store.Load("j1")
			if err != nil {
				t.Fatal(err)
			}
			st.Request.Cwd = unbornRepo(t)
			write(t, st.Request.Cwd, "user.txt", "user\n")
			if err := store.Save(st); err != nil {
				t.Fatal(err)
			}
			session.script = func(s *fakeSession) {
				if access == "write" {
					if err := os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("new\n"), 0600); err != nil {
						s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: err.Error()}
						return
					}
				}
				s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 10, OutputTokens: 2}}
				s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "DONE"}
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
			}
			r, err := RunJob(context.Background(), store, "j1", newFakeHarness(backend), WorkerConfig{})
			if err != nil || r.Status != StatusSucceeded || r.FinalText != "DONE" || r.InputTokens != 10 || r.OutputTokens != 2 {
				t.Fatalf("result=%+v err=%v", r, err)
			}
			want := 0
			if access == "write" {
				want = 1
			}
			if len(r.Diff.Files) != want || r.Diff.Added != want || r.Diff.Deleted != 0 {
				t.Fatalf("diff=%+v", r.Diff)
			}
			if access == "write" && (r.Diff.Files[0].Path != "out.txt" || !r.Diff.Files[0].New) {
				t.Fatalf("diff included preexisting files: %+v", r.Diff)
			}
			if contains(r.Warnings, "git stash") {
				t.Fatalf("warnings=%v", r.Warnings)
			}
			saved, err := store.Load("j1")
			if err != nil || saved.Status != StatusSucceeded {
				t.Fatalf("saved=%+v err=%v", saved, err)
			}
		})
	}
}

func TestRunJobUnbornDiffWarningDoesNotFailJob(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "harness failed"}[failed], func(t *testing.T) {
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
				// Introduce the unsupported path during the job. Preexisting
				// embedded repositories are excluded from both trees.
				err := os.WriteFile(filepath.Join(st.Request.Cwd, "a.txt"), []byte("new\n"), 0600)
				nested := filepath.Join(st.Request.Cwd, "nested")
				if err == nil {
					err = os.Mkdir(nested, 0700)
				}
				if err == nil {
					_, err = runGit(nested, "init", "-q")
				}
				if err != nil {
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: err.Error()}
					return
				}
				s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "DONE"}
				state := cockpit.StateTurnCompleted
				if failed {
					state = cockpit.StateTurnFailed
				}
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: state}
			}
			r, err := RunJob(context.Background(), store, "j1", newFakeHarness(backend), WorkerConfig{})
			want := StatusSucceeded
			if failed {
				want = StatusFailed
			}
			if err != nil || r.Status != want || r.FinalText != "DONE" || !contains(r.Warnings, "nested") || !contains(r.Warnings, "Changes") {
				t.Fatalf("result=%+v err=%v", r, err)
			}
			if len(r.Diff.Files) != 1 || r.Diff.Files[0].Path != "a.txt" || r.Diff.Added != 1 {
				t.Fatalf("partial diff lost valid files: %+v", r.Diff)
			}
		})
	}
}
