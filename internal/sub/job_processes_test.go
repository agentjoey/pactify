package sub

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/procx"
)

func TestJobMarkerEnvironmentInjection(t *testing.T) {
	for _, name := range []string{"codex", "kimi", "agy", "opencode"} {
		t.Run(name, func(t *testing.T) {
			store, session, backend := workerFixture(t, "write")
			t.Setenv("PACTIFY_JOB", "outer-job")
			session.script = func(s *fakeSession) {
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
			}
			h := newFakeHarness(backend)
			h.Name = name
			if _, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{}); err != nil {
				t.Fatal(err)
			}
			if got := childEnv(backend.gotOpts.Env, "PACTIFY_JOB"); got != "j1" {
				t.Fatalf("PACTIFY_JOB=%q, want j1", got)
			}
		})
	}
}

// The helper exits immediately after spawning a child in a new session. The
// worker never gets a chance to sample their ancestry.
func TestJobMarkerOrphanHelper(t *testing.T) {
	if os.Getenv("PACTIFY_TEST_JOB_ORPHAN") == "sleep" {
		time.Sleep(120 * time.Second)
		os.Exit(0)
	}
	if os.Getenv("PACTIFY_TEST_JOB_ORPHAN") != "1" {
		return
	}
	// Use an unprotected executable: macOS hides /bin/sleep's environment.
	child := exec.Command(os.Args[0], "-test.run=^TestJobMarkerOrphanHelper$")
	child.Env = append(os.Environ(), "PACTIFY_TEST_JOB_ORPHAN=sleep")
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	fmt.Print(child.Process.Pid)
	os.Exit(0)
}

func jobMarkerOrphan(t *testing.T, id string) procx.Identity {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestJobMarkerOrphanHelper$")
	cmd.Env = append(os.Environ(), "PACTIFY_TEST_JOB_ORPHAN=1", "PACTIFY_JOB="+id, "GORACE=atexit_sleep_ms=0")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("orphan parent: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	child, err := procx.Inspect(pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Signal(syscall.SIGKILL, false) })
	return child
}

func TestJobMarkerCleansUnrecordedOrphans(t *testing.T) {
	for _, ending := range []string{"finish", "finish-without-owned-temp", "cancel", "recover", "discard"} {
		t.Run(ending, func(t *testing.T) {
			store, st := worktreeFixture(t)
			if ending == "discard" {
				finishWorktree(t, store, st.ID, StatusSucceeded)
			} else {
				st.Status = StatusRunning // No live worker and no recorded processes.
				if err := store.Save(st); err != nil {
					t.Fatal(err)
				}
			}
			if ending != "finish-without-owned-temp" {
				if err := store.prepareJobTemp(context.Background(), st.ID); err != nil {
					t.Fatal(err)
				}
			}
			child := jobMarkerOrphan(t, st.ID)
			other := jobMarkerOrphan(t, "aj691-other-job")
			var err error
			switch ending {
			case "finish", "finish-without-owned-temp":
				err = store.Finish(st.ID, &Result{Status: StatusSucceeded}, "", time.Now())
			case "cancel":
				_, err = store.RequestCancel(st.ID)
			case "recover":
				_, err = store.Recover(st.ID)
			case "discard":
				err = store.DiscardWorktree(st.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if alive, err := child.Alive(); err != nil || alive {
				t.Errorf("unrecorded detached child survived %s: alive=%v err=%v", ending, alive, err)
			}
			if alive, err := other.Alive(); err != nil || !alive {
				t.Errorf("another job's child affected: alive=%v err=%v", alive, err)
			}
			if _, err := os.Stat(store.Dir(st.ID) + "/tmp"); !os.IsNotExist(err) {
				t.Errorf("temp survived cleanup: %v", err)
			}
		})
	}
}

func TestJobMarkerCleanupContinuesAfterTrackingError(t *testing.T) {
	child := jobMarkerOrphan(t, "aj691-tracking-error")
	err := (State{ID: "aj691-tracking-error", ProcessTrackingError: "tracking failed"}).KillHarnesses()
	if err == nil || !strings.Contains(err.Error(), "tracking failed") {
		t.Fatalf("tracking error lost: %v", err)
	}
	if alive, err := child.Alive(); err != nil || alive {
		t.Fatalf("tracking error skipped marker cleanup: %v %v", alive, err)
	}
}

func TestJobMarkerWorkerCleansUnrecordedOrphan(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%v", canceled), func(t *testing.T) {
			store, session, _ := workerFixture(t, "write")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var child procx.Identity
			backend := lifecycleBackend{start: func(_ context.Context, opts cockpit.StartOpts) (cockpit.Session, error) {
				// Model a tool's already-exited parent: neither it nor its detached
				// child is in the registered harness tree, regardless of polling.
				child = jobMarkerOrphan(t, childEnv(opts.Env, "PACTIFY_JOB"))
				return session, nil
			}}
			session.script = func(s *fakeSession) {
				if canceled {
					cancel()
				} else {
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				}
			}
			r, err := RunJob(ctx, store, "j1", Harness{Backend: func() cockpit.Backend { return backend }}, WorkerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			want := StatusSucceeded
			if canceled {
				want = StatusCanceled
			}
			if r.Status != want {
				t.Errorf("status=%s, want %s", r.Status, want)
			}
			if alive, err := child.Alive(); err != nil || alive {
				t.Fatalf("worker left unrecorded child: %v %v", alive, err)
			}
		})
	}
}
