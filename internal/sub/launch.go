package sub

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
)

func (store Store) StartWorker(id string) error { return store.startWorker(id, false) }

// StartDashboardWorker reaps children in the long-lived server and strips
// variables that configure serve or the frozen ref-ledger mirror.
func (store Store) StartDashboardWorker(id string) error { return store.startWorker(id, true) }

func (store Store) startWorker(id string, dashboard bool) error {
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	return store.startWorkerCommand(id, exec.Command(binary, "sub", "__worker", id), dashboard)
}

func (store Store) startWorkerCommand(id string, cmd *exec.Cmd, dashboard bool) error {
	in, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(store.Dir(id), "worker.out"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	if dashboard {
		cmd.Env = []string{}
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			if key != "PACTIFY_ALLOWED_ORIGINS" && key != "PACTIFY_LEDGER_REF" {
				cmd.Env = append(cmd.Env, entry)
			}
		}
	}
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return store.WithState(context.Background(), id, func(st *State) error {
		if Terminal(st.Status) || st.CancelRequested {
			return nil
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		identity, err := procx.Inspect(cmd.Process.Pid)
		if err == nil {
			st.PID = identity.PID
			st.Worker = &identity
			st.UpdatedAt = time.Now().UTC()
			err = store.Save(*st)
		}
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
		if dashboard {
			go func() { _ = cmd.Wait() }()
			return nil
		}
		return cmd.Process.Release()
	})
}
func (store Store) CancelJob(ctx context.Context, id string) (State, error) {
	final, err := store.FinalJob(id)
	if err != nil {
		return State{}, err
	}
	id = final.ID
	st, err := store.RequestCancel(id)
	if err != nil {
		return State{}, err
	}
	if st.Worker != nil && !Terminal(st.Status) {
		if err = st.Worker.Signal(syscall.SIGTERM, true); err != nil {
			return State{}, err
		}
	}
	deadline := time.Now().Add(24 * time.Second)
	for {
		// Even a terminal worker can leave a wedged harness behind.
		if Terminal(st.Status) {
			if err = st.KillHarnesses(); err != nil {
				return State{}, err
			}
			return st, nil
		}
		alive := false
		if st.Worker != nil {
			alive, err = st.Worker.Alive()
			if err != nil {
				return State{}, err
			}
		}
		if !alive || time.Now().After(deadline) {
			cleanupErr := st.KillHarnesses()
			if st.Worker != nil {
				cleanupErr = errors.Join(cleanupErr, st.Worker.Signal(syscall.SIGKILL, true))
			}
			if cleanupErr != nil {
				return State{}, cleanupErr
			}
			if st.CancelRequested {
				st.Status = StatusCanceled
				st.Error = "Task canceled (worker exited)"
				if alive {
					st.Error = "Task canceled (force terminated)"
				}
			} else if !alive {
				st.Status = StatusFailed
				st.Error = "job worker is absent or its process identity no longer matches"
			} else {
				st.Status = StatusCanceled
				st.Error = "Task canceled (force terminated)"
			}
			if err = store.SaveFallback(st); err != nil {
				return State{}, err
			}
			return st, nil
		}
		select {
		case <-ctx.Done():
			return State{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		st, err = store.Load(id)
		if err != nil {
			return State{}, err
		}
	}
}
func (store Store) SaveFallback(st State) error {
	result := Result{JobID: st.ID, Status: st.Status, Harness: st.Request.Harness, Access: st.Request.Access, ModelRequested: st.Request.Model, LogPath: store.LogPath(st.ID), Warnings: []string{st.Error}}
	if h, ok := Lookup(st.Request.Harness); ok && h.Experimental != "" {
		result.Warnings = append([]string{h.Experimental}, result.Warnings...)
	}
	return store.Finish(st.ID, &result, st.Error, time.Now())
}
