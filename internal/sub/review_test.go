package sub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/procx"
)

func readReviewResult(t *testing.T, store Store, id string) Result {
	t.Helper()
	data, err := os.ReadFile(store.ResultPath(id))
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestReviewRegisteredQueuedCancelFinishesBeforeClaim(t *testing.T) {
	for _, cause := range []string{"request", "context"} {
		t.Run(cause, func(t *testing.T) {
			store, _, _ := workerFixture(t, "write")
			identity, err := procx.Inspect(os.Getpid())
			if err != nil {
				t.Fatal(err)
			}
			st, err := store.Load("j1")
			if err != nil {
				t.Fatal(err)
			}
			st.PID, st.Worker = identity.PID, &identity
			if err := store.Save(st); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cause == "request" {
				st, err = store.RequestCancel(st.ID)
				if err != nil {
					t.Fatal(err)
				}
				if st.Status != StatusQueued || !st.CancelRequested {
					t.Fatalf("did not exercise registered queued cancellation: %+v", st)
				}
			} else {
				cancel()
			}
			backend := lifecycleBackend{start: func(context.Context, cockpit.StartOpts) (cockpit.Session, error) {
				t.Error("canceled worker started a harness")
				return nil, errors.New("unexpected startup")
			}}
			_, _ = RunJob(ctx, store, st.ID, Harness{Backend: func() cockpit.Backend { return backend }}, WorkerConfig{})
			saved, err := store.Load(st.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Status != StatusCanceled || !strings.Contains(saved.Error, "Task canceled") {
				t.Fatalf("queued cancellation not finalized: %+v", saved)
			}
			result := readReviewResult(t, store, st.ID)
			if result.Status != StatusCanceled {
				t.Fatalf("persisted result status=%s", result.Status)
			}
		})
	}
}

func TestReviewRecoverKeepsRegisteredQueuedCancellation(t *testing.T) {
	store := Store{Root: t.TempDir()}
	st, err := store.Create("queued", Request{Harness: "codex", Access: "write"})
	if err != nil {
		t.Fatal(err)
	}
	st.Worker = &procx.Identity{PID: 99999999, Start: "exited-worker"}
	st.PID = st.Worker.PID
	st.CancelRequested = true
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	st, err = store.Recover(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := readReviewResult(t, store, st.ID)
	if st.Status != StatusCanceled || result.Status != StatusCanceled || !strings.Contains(st.Error, "Task canceled") {
		t.Fatalf("canceled orphan became %s: %s; result=%s", st.Status, st.Error, result.Status)
	}
}

// These are real processes, either independent harness groups or deliberately
// invalid group leaders used to exercise the cleanup error path safely.
func reviewProcess(t *testing.T, group bool) (procx.Identity, <-chan struct{}) {
	t.Helper()
	cmd := exec.Command("sleep", "120")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: group}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("process cleanup did not finish")
		}
	})
	identity, err := procx.Inspect(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	return identity, done
}

func TestReviewOrphanReadersKillRegisteredHarness(t *testing.T) {
	for _, reader := range []string{"recover", "cancel"} {
		for _, writeFails := range []bool{false, true} {
			t.Run(reader+map[bool]string{false: "", true: "/publication-fails"}[writeFails], func(t *testing.T) {
				harness, done := reviewProcess(t, true)
				store := Store{Root: t.TempDir()}
				st, err := store.Create("orphan", Request{Harness: "codex", Access: "write"})
				if err != nil {
					t.Fatal(err)
				}
				st.Status = StatusRunning
				st.Worker = &procx.Identity{PID: 99999999, Start: "exited-worker"}
				st.PID = st.Worker.PID
				st.Processes = []procx.Identity{harness}
				if err := store.Save(st); err != nil {
					t.Fatal(err)
				}
				if writeFails {
					if err := os.Mkdir(store.ResultPath(st.ID), 0700); err != nil {
						t.Fatal(err)
					}
				}
				if reader == "recover" {
					st, err = store.Recover(st.ID)
				} else {
					st, err = store.RequestCancel(st.ID)
				}
				if (err != nil) != writeFails {
					t.Fatalf("publication error=%v, want failure=%v", err, writeFails)
				}
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("orphan harness is still running after worker recovery")
				}
				if !writeFails {
					result := readReviewResult(t, store, "orphan")
					if st.Status != StatusFailed || result.Status != StatusFailed {
						t.Fatalf("orphan status=%s result=%s", st.Status, result.Status)
					}
				}
			})
		}
	}
}

func TestReviewKillHarnessesContinuesAfterErrors(t *testing.T) {
	first, _ := reviewProcess(t, false)
	second, _ := reviewProcess(t, false)
	valid, done := reviewProcess(t, true)
	st := State{Processes: []procx.Identity{first, second, valid}}
	err := st.KillHarnesses()
	if err == nil || strings.Count(err.Error(), "not its process group leader") != 2 {
		t.Errorf("cleanup did not retain both errors: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a preceding cleanup error skipped the remaining harness")
	}
}

func TestReviewFallbackKeepsExperimentalWarning(t *testing.T) {
	for _, path := range []string{"recover", "cancel"} {
		t.Run(path, func(t *testing.T) {
			store := Store{Root: t.TempDir()}
			st, err := store.Create("kimi", Request{Harness: "kimi", Access: "write"})
			if err != nil {
				t.Fatal(err)
			}
			st.CreatedAt = time.Now().Add(-time.Minute)
			if err := store.Save(st); err != nil {
				t.Fatal(err)
			}
			if path == "recover" {
				st, err = store.Recover(st.ID)
			} else {
				st, err = store.RequestCancel(st.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			result := readReviewResult(t, store, st.ID)
			want := "kimi is an experimental harness (Phase 0: large tasks completed in one attempt 6/10; read-only access is not offered)"
			if len(result.Warnings) != 2 || result.Warnings[0] != want || result.Warnings[1] != st.Error || st.Error == want {
				t.Fatalf("fallback warnings=%v state error=%q", result.Warnings, st.Error)
			}
		})
	}
}
