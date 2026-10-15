package sub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestResumeRunSelectsOlderWorktreeAndDirectParent(t *testing.T) {
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	s, first := worktreeFixture(t)
	finishWorktree(t, s, first.ID, StatusSucceeded)
	other, err := s.CreateRun("other", first.Request, true, false)
	if err != nil {
		t.Fatal(err)
	}
	finishWorktree(t, s, other.ID, StatusSucceeded)
	for i, parent := range []string{first.ID, first.ID, "turn-1"} {
		id := fmt.Sprintf("turn-%d", i)
		next, err := s.ResumeRun(id, first.Request, i%2 == 0, parent)
		if err != nil {
			t.Fatal(err)
		}
		if next.ResumeOf != parent || next.Worktree.Path != first.Worktree.Path || next.Worktree.BaseCommit != first.Worktree.BaseCommit || next.Request.Resume != "session-"+parent {
			t.Fatalf("wrong resume: %+v", next)
		}
		if _, err := s.ResumeRun("busy", first.Request, false, first.ID); err == nil || !strings.Contains(err.Error(), "still running") {
			t.Fatalf("concurrent resume: %v", err)
		}
		finishWorktree(t, s, next.ID, StatusCanceled)
	}
	first, _ = s.Load(first.ID)
	turn0, _ := s.Load("turn-0")
	turn1, _ := s.Load("turn-1")
	turn2, _ := s.Load("turn-2")
	if got := s.Summarize(first, []State{first, turn0, turn1, turn2}, time.Now()).ResumedBy; !reflect.DeepEqual(got, []string{"turn-0", "turn-1"}) {
		t.Fatalf("direct children=%v", got)
	}
	if err := s.DiscardWorktree("turn-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResumeRun("discarded", first.Request, false, first.ID); err == nil || !strings.Contains(err.Error(), "discarded") {
		t.Fatalf("discarded resume: %v", err)
	}
}

func TestResumeRunInheritsOriginalDirectory(t *testing.T) {
	s := Store{Root: t.TempDir()}
	previous, err := s.Create("previous", Request{Harness: "codex", Cwd: repo(t)})
	if err != nil {
		t.Fatal(err)
	}
	finishWorktree(t, s, previous.ID, StatusSucceeded)
	next, err := s.ResumeRun("next", Request{Cwd: repo(t)}, false, previous.ID)
	if err != nil || next.Request.Harness != "codex" || next.Request.Cwd != previous.Request.Cwd {
		t.Fatalf("selected job's cwd/harness lost: %+v %v", next, err)
	}
}

func TestResolveResumeJobExactAndPrefixes(t *testing.T) {
	s := Store{Root: t.TempDir()}
	for _, id := range []string{"20261015-010000-af34c8", "20261015-020000-af3456", "af34", "true", "false", "latest", "codex"} {
		if _, err := s.Create(id, Request{}); err != nil {
			t.Fatal(err)
		}
	}
	for selector, want := range map[string]string{
		"20261015-010000-af34c8": "20261015-010000-af34c8", "20261015-01": "20261015-010000-af34c8", "af34c": "20261015-010000-af34c8",
		"af34": "af34", "true": "true", "false": "false", "latest": "latest", "codex": "codex",
	} {
		st, err := s.ResolveResumeJob(selector)
		if err != nil || st.ID != want {
			t.Errorf("%s: %+v %v", selector, st, err)
		}
	}
	if _, err := s.ResolveResumeJob("af3"); err == nil || !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), "20261015-010000-af34c8") || !strings.Contains(err.Error(), "20261015-020000-af3456") || !strings.Contains(err.Error(), "af34") {
		t.Fatalf("ambiguity: %v", err)
	}
	if _, err := s.ResolveResumeJob("missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing: %v", err)
	}
}

func TestResumeRunMissingWorktreeAndSession(t *testing.T) {
	s, st := worktreeFixture(t)
	finishWorktree(t, s, st.ID, StatusSucceeded)
	if err := os.RemoveAll(st.Worktree.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResumeRun("missing", st.Request, false, st.ID); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("missing worktree: %v", err)
	}
	for _, data := range []string{"{}", "{"} {
		if err := os.WriteFile(s.ResultPath(st.ID), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ResumeRun("no-session", st.Request, false, st.ID); err == nil || !strings.Contains(err.Error(), "session id") {
			t.Fatalf("missing/corrupt session: %v", err)
		}
	}
}

func TestSpecifiedResumeRunsEveryHarness(t *testing.T) {
	for _, name := range []string{"codex", "kimi", "agy", "opencode"} {
		for _, isolated := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/worktree=%v", name, isolated), func(t *testing.T) {
				store, sess, backend := workerFixture(t, "write")
				// All source homes and usage paths stay in fixture directories.
				t.Setenv("HOME", t.TempDir())
				t.Setenv("XDG_DATA_HOME", t.TempDir())
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				t.Setenv("KIMI_CODE_HOME", t.TempDir())
				base, _ := store.Load("j1")
				req := base.Request
				req.Harness = name
				req.WithUserConfig = true
				first, err := store.CreateRun("first", req, isolated, false)
				if err != nil {
					t.Fatal(err)
				}
				first.HarnessHome = os.Getenv("HOME")
				first.HarnessHomes = map[string]string{"fixture": filepath.Join(os.Getenv("HOME"), "extra")}
				if err := store.Save(first); err != nil {
					t.Fatal(err)
				}
				finishWorktree(t, store, first.ID, StatusFailed)
				next, err := store.ResumeRun("next", req, isolated, first.ID)
				if err != nil {
					t.Fatal(err)
				}
				if next.ResumeOf != first.ID {
					t.Fatal("lost resume parent", next)
				}
				h, _ := Lookup(name)
				if h.Home != nil && (next.HarnessHome != first.HarnessHome || !reflect.DeepEqual(next.HarnessHomes, first.HarnessHomes)) {
					t.Fatal("lost recorded harness homes", next)
				}
				// Keep each harness's actual resume configuration/worker dispatch,
				// substituting only the backend and external CLI verification.
				h.Backend = func() cockpit.Backend { return backend }
				h.Version, h.Verify, h.Conceal = nil, nil, nil
				sess.script = func(s *fakeSession) {
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				}
				r, err := RunJob(context.Background(), store, next.ID, h, WorkerConfig{})
				wantCwd := req.Cwd
				if isolated {
					wantCwd = first.Worktree.Cwd
				}
				if err != nil || r.Status != StatusSucceeded || backend.resumed != "session-first" || backend.gotOpts.RepoDir != wantCwd {
					t.Fatalf("result=%+v resume=%s cwd=%s err=%v", r, backend.resumed, backend.gotOpts.RepoDir, err)
				}
				if isolated {
					if err := store.DiscardWorktree(next.ID); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
