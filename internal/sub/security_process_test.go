package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/procx"
)

func TestSecurityDescendantHelper(t *testing.T) {
	path := os.Getenv("PACTIFY_TEST_DESCENDANT")
	if path == "" {
		return
	}
	child := exec.Command("sleep", "120")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		child.Process.Kill()
		os.Exit(3)
	}
	child.Wait()
	os.Exit(0)
}
func startSecurityTree(t *testing.T, start func(*exec.Cmd) error) (procx.Identity, procx.Identity) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "child")
	cmd := exec.Command(os.Args[0], "-test.run=^TestSecurityDescendantHelper$")
	cmd.Env = append(os.Environ(), "PACTIFY_TEST_DESCENDANT="+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := start(cmd); err != nil {
		t.Fatal(err)
	}
	parent, err := procx.Inspect(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	var child procx.Identity
	t.Cleanup(func() { child.Signal(syscall.SIGKILL, false); cmd.Process.Kill(); cmd.Wait() })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, e := os.ReadFile(path)
		if e == nil {
			pid, _ := strconv.Atoi(string(b))
			child, e = procx.Inspect(pid)
			if e == nil {
				return parent, child
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child not started")
	return parent, child
}
func TestSecurityRecoveryKillsSavedDetachedDescendant(t *testing.T) {
	for _, reader := range []string{"recover", "cancel"} {
		t.Run(reader, func(t *testing.T) {
			store := Store{Root: t.TempDir()}
			st, err := store.Create("orphan", Request{Harness: "codex"})
			if err != nil {
				t.Fatal(err)
			}
			parent, child := startSecurityTree(t, func(cmd *exec.Cmd) error { return cmd.Start() })
			// Record a snapshot exactly as the worker does; kill the parent to lose ancestry.
			data, _ := json.Marshal(map[string]any{"descendants": []procx.Identity{child}})
			if err := json.Unmarshal(data, &st); err != nil {
				t.Fatal(err)
			}
			st.Status = StatusRunning
			st.Processes = []procx.Identity{parent}
			st.Worker = &procx.Identity{PID: os.Getpid(), Start: "wrong-birth"}
			if err := store.Save(st); err != nil {
				t.Fatal(err)
			}
			if err := store.prepareJobTemp(context.Background(), st.ID); err != nil {
				t.Fatal(err)
			}
			if err := parent.Signal(syscall.SIGKILL, true); err != nil {
				t.Fatal(err)
			}
			if reader == "recover" {
				_, err = store.Recover(st.ID)
			} else {
				_, err = store.RequestCancel(st.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			alive, e := child.Alive()
			if e != nil || alive {
				t.Fatalf("detached descendant survived: %v %v", alive, e)
			}
			if _, err := os.Stat(filepath.Join(store.Dir(st.ID), "tmp")); !os.IsNotExist(err) {
				t.Fatalf("temp not cleaned after descendants: %v", err)
			}
		})
	}
}
func TestSecurityWorkerPersistsDescendants(t *testing.T) {
	store, s, _ := workerFixture(t, "write")
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	b := lifecycleBackend{start: func(ctx context.Context, _ cockpit.StartOpts) (cockpit.Session, error) {
		_, child := startSecurityTree(t, func(cmd *exec.Cmd) error { return procx.Start(ctx, cmd) })
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			data, err := os.ReadFile(filepath.Join(store.Dir("j1"), "state.json"))
			if err != nil {
				return nil, err
			}
			var saved struct {
				Descendants []procx.Identity `json:"descendants"`
			}
			if err := json.Unmarshal(data, &saved); err != nil {
				return nil, err
			}
			for _, p := range saved.Descendants {
				if p == child {
					return s, nil
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		return nil, fmt.Errorf("worker never persisted detached descendant identity")
	}}
	_, err := RunJob(context.Background(), store, "j1", Harness{Backend: func() cockpit.Backend { return b }}, WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSecurityWorktreeRejectsLiveSavedDescendant(t *testing.T) {
	store, st := worktreeFixture(t)
	finishWorktree(t, store, st.ID, StatusFailed)
	child, _ := reviewProcess(t, true)
	st, err := store.Load(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	st.Descendants = []procx.Identity{child}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	if err := store.worktreeIdle(st.Worktree); err == nil {
		t.Fatal("worktree accepted while saved descendant is alive")
	}
}
