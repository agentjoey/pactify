package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

// Re-exec only this peer, never a vendor CLI. It models a worker in teardown or
// one unable to cooperate with cancellation, using real process groups/signals.
func TestSubReviewWorkerPeer(t *testing.T) {
	mode := os.Getenv("SUB_REVIEW_WORKER")
	if mode == "" {
		return
	}
	signals := make(chan os.Signal, 1)
	if mode == "wedged" {
		signal.Ignore(syscall.SIGTERM)
	} else {
		signal.Notify(signals, syscall.SIGTERM)
	}
	if err := os.WriteFile(os.Getenv("SUB_REVIEW_READY"), []byte("ready"), 0600); err != nil {
		os.Exit(4)
	}
	if mode == "wedged" {
		time.Sleep(2 * time.Minute)
		os.Exit(0)
	}
	<-signals
	if mode == "exit-on-term" {
		os.Exit(0)
	}
	time.Sleep(17 * time.Second)
	store, err := sub.DefaultStore()
	if err != nil {
		os.Exit(4)
	}
	st, err := store.Load("worker")
	if err != nil {
		os.Exit(4)
	}
	result := sub.Result{JobID: st.ID, Status: sub.StatusCanceled, Harness: st.Request.Harness, Warnings: []string{"archive completed"}}
	if err := store.Finish(st.ID, &result, "Task canceled", time.Now()); err != nil {
		os.Exit(4)
	}
	os.Exit(0)
}

func startSubReviewWorker(t *testing.T, mode string, status sub.Status) (sub.Store, sub.State, <-chan struct{}) {
	t.Helper()
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Create("worker", sub.Request{Harness: "codex", Access: "write"})
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command(exe, "-test.run=^TestSubReviewWorkerPeer$")
	cmd.Env = append(os.Environ(), "SUB_REVIEW_WORKER="+mode, "SUB_REVIEW_READY="+ready)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err = store.WithState(context.Background(), st.ID, func(current *sub.State) error {
		if err := cmd.Start(); err != nil {
			return err
		}
		identity, err := procx.Inspect(cmd.Process.Pid)
		if err != nil {
			return err
		}
		current.Worker = &identity
		current.PID = identity.PID
		current.Status = status
		st = *current
		return store.Save(*current)
	})
	if err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = st.Worker.Signal(syscall.SIGKILL, true)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("worker cleanup did not finish")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case <-done:
			t.Fatal("worker exited before ready")
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return store, st, done
}

func subReviewCommand(t *testing.T) *cobra.Command {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	c := &cobra.Command{}
	c.SetContext(ctx)
	c.SetOut(&bytes.Buffer{})
	return c
}

func TestReviewCancelQueuedRegisteredWorkerHasCancelReason(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	store, st, _ := startSubReviewWorker(t, "exit-on-term", sub.StatusQueued)
	if code, err := subCancel(subReviewCommand(t), st.ID); code != 0 || err != nil {
		t.Fatalf("cancel: %d %v", code, err)
	}
	st, err := store.Load(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != sub.StatusCanceled || !strings.Contains(st.Error, "Task canceled") {
		t.Fatalf("queued cancel has wrong outcome: %s %q", st.Status, st.Error)
	}
}

func TestReviewCancelAllowsSlowCleanup(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	store, st, _ := startSubReviewWorker(t, "cleanup", sub.StatusRunning)
	if code, err := subCancel(subReviewCommand(t), st.ID); code != 0 || err != nil {
		t.Fatalf("cancel: %d %v", code, err)
	}
	data, err := os.ReadFile(store.ResultPath(st.ID))
	if err != nil {
		t.Fatal(err)
	}
	var result sub.Result
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != sub.StatusCanceled || len(result.Warnings) != 1 || result.Warnings[0] != "archive completed" {
		t.Fatalf("force kill interrupted cleanup: %s", data)
	}
}

func TestReviewHarnessErrorDoesNotSkipWorkerKill(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	store, st, done := startSubReviewWorker(t, "wedged", sub.StatusRunning)
	// This child inherits the test runner's group and cannot be its leader.
	invalidProcess := exec.Command("sleep", "120")
	if err := invalidProcess.Start(); err != nil {
		t.Fatal(err)
	}
	invalidDone := make(chan struct{})
	go func() { _ = invalidProcess.Wait(); close(invalidDone) }()
	t.Cleanup(func() { _ = invalidProcess.Process.Kill(); <-invalidDone })
	invalid, err := procx.Inspect(invalidProcess.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	st.Processes = []procx.Identity{invalid}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	if code, err := subCancel(subReviewCommand(t), st.ID); code != 1 || err == nil {
		t.Fatalf("expected group cleanup error: %d %v", code, err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("harness cleanup error skipped the worker SIGKILL")
	}
}

func TestReviewCLIOrphanReadersStopHarness(t *testing.T) {
	bin := buildPactify(t)
	for _, reader := range []string{"status", "wait", "result"} {
		t.Run(reader, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			store, st, workerDone := startSubReviewWorker(t, "wedged", sub.StatusRunning)
			harness := exec.Command("sleep", "120")
			harness.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := harness.Start(); err != nil {
				t.Fatal(err)
			}
			harnessDone := make(chan struct{})
			go func() { _ = harness.Wait(); close(harnessDone) }()
			t.Cleanup(func() { _ = harness.Process.Kill(); <-harnessDone })
			identity, err := procx.Inspect(harness.Process.Pid)
			if err != nil {
				t.Fatal(err)
			}
			st.Processes = []procx.Identity{identity}
			if err := store.Save(st); err != nil {
				t.Fatal(err)
			}
			if err := st.Worker.Signal(syscall.SIGKILL, true); err != nil {
				t.Fatal(err)
			}
			<-workerDone
			out, code := runCLI(t, bin, t.TempDir(), "", "sub", reader, st.ID, "--json")
			expected := 1
			if reader == "status" {
				expected = 0
			}
			if code != expected || !strings.Contains(out, `"failed"`) {
				t.Fatalf("%s: %d %s", reader, code, out)
			}
			select {
			case <-harnessDone:
			case <-time.After(time.Second):
				t.Fatal("CLI reader left orphan harness alive")
			}
		})
	}
}
