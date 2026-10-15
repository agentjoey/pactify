package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
	"golang.org/x/sys/unix"
)

type acpProcessRecord struct {
	Identity procx.Identity
	PPID     int
	PGID     int
	SID      int
}

func acpSelfRecord() acpProcessRecord {
	identity, _ := procx.Inspect(os.Getpid())
	pgid, _ := syscall.Getpgid(os.Getpid())
	sid, _ := unix.Getsid(os.Getpid())
	return acpProcessRecord{identity, os.Getppid(), pgid, sid}
}

// No vendor executable, user home or credential is accessed by this peer.
func TestACPDetachedProcessHelper(t *testing.T) {
	role := os.Getenv("ACP_PROCESS_ROLE")
	if role == "" {
		return
	}
	path := os.Getenv("ACP_PROCESS_RECORD")
	if role == "child" {
		signal.Ignore(syscall.SIGTERM)
		record, _ := json.Marshal(acpSelfRecord())
		if os.WriteFile(path+".child", record, 0600) != nil {
			os.Exit(40)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	in := json.NewDecoder(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	var child *exec.Cmd
	var promptID json.RawMessage
	for {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if in.Decode(&req) != nil {
			os.Exit(0)
		}
		result := any(map[string]any{})
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1}
		case "session/new":
			result = map[string]any{"sessionId": "sid"}
		case "session/prompt":
			promptID = req.ID
			exe, _ := os.Executable()
			child = exec.Command(exe, "-test.run=^TestACPDetachedProcessHelper$")
			child.Env = append(os.Environ(), "ACP_PROCESS_ROLE=child")
			child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if child.Start() != nil {
				os.Exit(41)
			}
			deadline := time.Now().Add(5 * time.Second)
			var record acpProcessRecord
			for {
				data, err := os.ReadFile(path + ".child")
				if err == nil && json.Unmarshal(data, &record) == nil {
					break
				}
				if time.Now().After(deadline) {
					os.Exit(42)
				}
				time.Sleep(5 * time.Millisecond)
			}
			records, _ := json.Marshal([]acpProcessRecord{acpSelfRecord(), record})
			if os.WriteFile(path, records, 0600) != nil {
				os.Exit(43)
			}
			out.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "sid", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "ready"}}}})
			continue
		case "session/cancel":
			switch os.Getenv("ACP_PROCESS_MODE") {
			case "native":
				time.Sleep(200 * time.Millisecond)
				if child.Process.Kill() != nil || child.Wait() == nil {
					os.Exit(44)
				}
				if os.WriteFile(path+".cleaned", []byte("native cleanup"), 0600) != nil {
					os.Exit(45)
				}
				out.Encode(map[string]any{"jsonrpc": "2.0", "id": promptID, "result": map[string]string{"stopReason": "cancelled"}})
			case "orphan":
				os.Exit(0)
			}
			continue
		}
		if len(req.ID) > 0 {
			out.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		}
	}
}

func TestACPCloseCollectsDetachedDescendants(t *testing.T) {
	for _, mode := range []string{"native", "orphan", "stubborn", "close", "context"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "processes.json")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c, err := Spawn(ctx, exe, []string{"-test.run=^TestACPDetachedProcessHelper$"}, []string{"ACP_PROCESS_ROLE=agent", "ACP_PROCESS_MODE=" + mode, "ACP_PROCESS_RECORD=" + path}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			probeCtx, stopProbe := context.WithTimeout(context.Background(), 10*time.Second)
			defer stopProbe()
			if _, err = c.Initialize(probeCtx); err != nil {
				t.Fatal(err)
			}
			sid, err := c.NewSession(probeCtx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ready := make(chan struct{}, 1)
			c.OnSessionUpdateFor(sid, func(SessionUpdate) {
				select {
				case ready <- struct{}{}:
				default:
				}
			})
			workCtx, stopWork := context.WithCancel(probeCtx)
			defer stopWork()
			promptDone := make(chan error, 1)
			go func() { _, e := c.Prompt(workCtx, sid, "run detached tool"); promptDone <- e }()
			select {
			case <-ready:
			case <-probeCtx.Done():
				t.Fatal("peer did not start tool")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var records []acpProcessRecord
			if err = json.Unmarshal(data, &records); err != nil || len(records) != 2 {
				t.Fatalf("records=%s err=%v", data, err)
			}
			parent, child := records[0], records[1]
			t.Cleanup(func() { _ = child.Identity.Signal(syscall.SIGKILL, false) })
			t.Logf("ACP pid=%d ppid=%d pgid=%d sid=%d; tool pid=%d ppid=%d pgid=%d sid=%d", parent.Identity.PID, parent.PPID, parent.PGID, parent.SID, child.Identity.PID, child.PPID, child.PGID, child.SID)
			if child.PPID != parent.Identity.PID || child.PGID == parent.PGID || child.SID == parent.SID {
				t.Fatal("fixture did not detach process group and session")
			}
			// Match worker order: local Prompt returns before the remote turn has ended.
			stopWork()
			<-promptDone
			if mode == "context" {
				cancel()
			} else if mode != "close" {
				if err = c.Cancel(sid); err != nil {
					t.Fatal(err)
				}
			}
			if err = c.Close(); err != nil {
				t.Fatal(err)
			}
			alive, e := child.Identity.Alive()
			if e != nil || alive {
				t.Fatalf("detached tool survived Close: %+v alive=%t err=%v", child, alive, e)
			}
			if mode == "native" {
				if _, err = os.Lstat(path + ".cleaned"); err != nil {
					t.Fatal("Close killed ACP before native cancellation cleanup", err)
				}
			}
		})
	}
}

func TestSpawnCancellationDuringLaunchRegistration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	launchErr := fmt.Errorf("fixture registration failure")
	ctx = procx.WithLauncher(ctx, func(cmd *exec.Cmd) error {
		if err := cmd.Start(); err != nil {
			return err
		}
		cancel()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return launchErr
	})
	done := make(chan error, 1)
	go func() {
		_, err := Spawn(ctx, "/bin/sh", []string{"-c", "exec sleep 300"}, nil, t.TempDir())
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, launchErr) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation deadlocked with launch registration failure")
	}
}

func TestSpawnAlreadyCanceledDoesNotLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, err := Spawn(ctx, "/bin/sh", []string{"-c", "exit 0"}, nil, t.TempDir())
	if c != nil {
		defer c.Close()
	}
	if c != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("client=%v err=%v", c, err)
	}
}
