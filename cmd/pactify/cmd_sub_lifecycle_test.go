package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func TestSubLifecyclePeer(t *testing.T) {
	if os.Getenv("SUB_LIFECYCLE_PEER") != "1" {
		return
	}
	signal.Ignore(syscall.SIGHUP, syscall.SIGTERM)
	in := json.NewDecoder(os.Stdin)
	for {
		var m struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if in.Decode(&m) != nil {
			os.Exit(0)
		}
		if m.ID == 0 {
			continue
		}
		result := any(map[string]any{})
		if m.Method == "thread/start" {
			result = map[string]any{"thread": map[string]any{"id": "stuck-thread"}}
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		if m.Method == "turn/start" {
			_ = os.WriteFile(os.Getenv("SUB_LIFECYCLE_READY"), []byte(strconv.Itoa(os.Getpid())), 0600)
			// A wedged harness ignores stdin, reproducing a worker unable to close it.

			for i := 0; i < 1200; i++ {
				_ = os.WriteFile(os.Getenv("SUB_LIFECYCLE_READY")+".heartbeat", []byte(strconv.Itoa(i)), 0600)
				time.Sleep(100 * time.Millisecond)
			}
			os.Exit(0)
		}
	}
}

func TestSubLifecycleForceCancelKillsHarnessGroup(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("SUB_LIFECYCLE_PEER", "1")
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv("SUB_LIFECYCLE_READY", ready)
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shim := t.TempDir()
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "' -test.run='^TestSubLifecyclePeer$'\n"
	if err := os.WriteFile(filepath.Join(shim, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, code := runCLI(t, bin, repo, "brief", "sub", "run", "codex", "--wait", "0s", "--json")
	var pending struct {
		JobID string `json:"jobId"`
	}
	if code != 3 || json.Unmarshal([]byte(out), &pending) != nil {
		t.Fatalf("run: %d %s", code, out)
	}
	store, _ := sub.DefaultStore()
	deadline := time.Now().Add(10 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(ready)
		pid, _ = strconv.Atoi(string(data))
		if pid > 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid < 2 {
		t.Fatal("harness did not start")
	}
	defer syscall.Kill(-pid, syscall.SIGKILL)
	st, _ := store.Load(pending.JobID)
	defer syscall.Kill(-st.PID, syscall.SIGKILL)
	if err := syscall.Kill(st.PID, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "cancel", st.ID)
	if code != 0 {
		t.Fatalf("cancel: %d %s", code, out)
	}
	before, e := os.ReadFile(ready + ".heartbeat")
	if e != nil {
		t.Fatal(e)
	}
	time.Sleep(300 * time.Millisecond)
	after, e := os.ReadFile(ready + ".heartbeat")
	if e != nil {
		t.Fatal(e)
	}
	if string(before) != string(after) {
		t.Fatalf("harness %d survived forced cancel (heartbeat %s -> %s)", pid, before, after)
	}
	out, code = runCLI(t, bin, repo, "", "sub", "wait", st.ID, "--json")
	if code != 1 || !strings.Contains(out, `"canceled"`) {
		t.Fatalf("wait: %d %s", code, out)
	}
}

func TestSubLifecycleCancelRejectsReusedPID(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	unrelated := exec.Command("sleep", "120")
	unrelated.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- unrelated.Wait() }()
	defer func() {
		_ = unrelated.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}()
	store, _ := sub.DefaultStore()
	st, err := store.Create("reused", sub.Request{Harness: "codex", Access: "write"})
	if err != nil {
		t.Fatal(err)
	}
	st.Status = sub.StatusRunning
	st.PID = unrelated.Process.Pid
	data, _ := json.Marshal(st)
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	raw["worker"] = map[string]any{"pid": st.PID, "start": "a different process start"}
	data, _ = json.Marshal(raw)
	if err := os.WriteFile(filepath.Join(store.Dir(st.ID), "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	c := &cobra.Command{}
	c.SetContext(ctx)
	c.SetOut(&bytes.Buffer{})
	_, _ = subCancel(c, st.ID)
	select {
	case e := <-done:
		t.Fatalf("unrelated process was signaled: %v", e)
	default:
	}
	st, _ = store.Load(st.ID)
	if st.Status != sub.StatusFailed || !strings.Contains(st.Error, "worker") {
		t.Fatalf("stale identity not failed: %+v", st)
	}
}

func TestSubLifecycleOrphansRecoveredByWaitAndStatus(t *testing.T) {
	for _, reader := range []string{"wait", "status", "list"} {
		for _, status := range []sub.Status{sub.StatusQueued, sub.StatusRunning} {
			t.Run(fmt.Sprintf("%s/%s", reader, status), func(t *testing.T) {
				t.Setenv("PACTIFY_HOME", t.TempDir())
				store, _ := sub.DefaultStore()
				st, err := store.Create("orphan", sub.Request{Harness: "codex", Access: "write"})
				if err != nil {
					t.Fatal(err)
				}
				st.Status = status
				st.CreatedAt = time.Now().Add(-time.Minute)
				st.UpdatedAt = st.CreatedAt
				if status == sub.StatusRunning {
					st.PID = 99999999
				}
				if err := store.Save(st); err != nil {
					t.Fatal(err)
				}
				c := &cobra.Command{}
				c.SetContext(context.Background())
				var out bytes.Buffer
				c.SetOut(&out)
				if reader == "wait" {
					code, e := subWait(c, store, st.ID, 400*time.Millisecond, true, false)
					if e != nil || code != 1 {
						t.Errorf("wait: %d %v %s", code, e, out.String())
					}
				} else {
					ids := []string{st.ID}
					if reader == "list" {
						ids = nil
					}
					if code, e := subStatus(c, ids, true); e != nil || code != 0 {
						t.Fatalf("status: %d %v", code, e)
					}
				}
				st, _ = store.Load(st.ID)
				if st.Status != sub.StatusFailed || !strings.Contains(st.Error, "worker") {
					t.Fatalf("orphan not recovered: %s %s", st.Status, st.Error)
				}
				data, err := os.ReadFile(store.ResultPath(st.ID))
				var r sub.Result
				if err != nil || json.Unmarshal(data, &r) != nil || r.Status != sub.StatusFailed {
					t.Fatalf("failed result missing: %s %v", data, err)
				}
			})
		}
	}
}

// Hold the claim lock while a real __worker is launched; fail either its running
// state write or its terminal output write, then verify wait recovers after exit.
func TestSubLifecycleRecoversPersistenceFailures(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	for _, phase := range []string{"running", "terminal"} {
		t.Run(phase, func(t *testing.T) {
			store, _ := sub.DefaultStore()
			st, err := store.Create("failure-"+phase, sub.Request{Harness: "fake", Access: "write", Cwd: repo, Brief: "work"})
			if err != nil {
				t.Fatal(err)
			}
			worker := exec.Command(bin, "sub", "__worker", st.ID)
			worker.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			var stderr bytes.Buffer
			worker.Stderr = &stderr
			err = store.WithState(context.Background(), st.ID, func(current *sub.State) error {
				if err := worker.Start(); err != nil {
					return err
				}
				identity, e := procx.Inspect(worker.Process.Pid)
				if e != nil {
					return e
				}
				current.PID = identity.PID
				current.Worker = &identity
				if e = store.Save(*current); e != nil {
					return e
				}
				if phase == "running" {
					return os.Chmod(store.Dir(st.ID), 0500)
				}
				return os.Mkdir(store.ResultPath(st.ID), 0700)
			})
			if err != nil {
				if worker.Process != nil {
					_ = worker.Process.Kill()
					_ = worker.Wait()
				}
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(store.Dir(st.ID), 0700) })
			if err := worker.Wait(); err == nil {
				t.Fatalf("expected persistence failure, stderr=%s", stderr.String())
			}
			if phase == "running" {
				if err := os.Chmod(store.Dir(st.ID), 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(store.ResultPath(st.ID)); err != nil {
					t.Fatal(err)
				}
			}
			before, e := store.Load(st.ID)
			if e != nil {
				t.Fatal(e)
			}
			expected := sub.StatusQueued
			if phase == "terminal" {
				expected = sub.StatusRunning
			}
			if before.Status != expected {
				t.Fatalf("did not exercise %s persistence failure: %+v, stderr=%s", phase, before, stderr.String())
			}
			out, code := runCLI(t, bin, repo, "", "sub", "wait", st.ID, "--json")
			if code != 1 || !strings.Contains(out, `"failed"`) || !strings.Contains(out, "worker") {
				t.Fatalf("wait: %d %s", code, out)
			}
		})
	}
}

func TestSubLifecycleCancelCleansOrphanHarness(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	child := exec.Command("sleep", "120")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	defer func() { _ = child.Process.Kill() }()
	identity, err := procx.Inspect(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := sub.DefaultStore()
	st, err := store.Create("orphan-with-harness", sub.Request{Harness: "codex", Access: "write"})
	if err != nil {
		t.Fatal(err)
	}
	st.Status = sub.StatusRunning
	st.PID = 99999999
	st.Worker = &procx.Identity{PID: st.PID, Start: "dead-worker"}
	st.Processes = []procx.Identity{identity}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	c := &cobra.Command{}
	c.SetContext(context.Background())
	c.SetOut(&bytes.Buffer{})
	if code, err := subCancel(c, st.ID); err != nil || code != 0 {
		t.Fatalf("cancel: %d %v", code, err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel left orphan harness alive after worker disappeared")
	}
}
