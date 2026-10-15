package sub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/lockx"
	"github.com/agentjoey/pactify/internal/procx"
)

func Terminal(s Status) bool { return s == StatusSucceeded || s == StatusFailed || s == StatusCanceled }

// WithState serializes lifecycle read-modify-write across workers and CLI processes.
// Callbacks use Save only while holding this lock; never nest WithState calls.
func (s Store) WithState(ctx context.Context, id string, f func(*State) error) error {
	if _, err := s.Load(id); err != nil {
		return err
	}
	release, err := lockx.Acquire(ctx, filepath.Join(s.Dir(id), "state.lock"))
	if err != nil {
		return err
	}
	defer release()
	st, err := s.Load(id)
	if err != nil {
		return err
	}
	return f(&st)
}

func (s Store) claim(ctx context.Context, id string, now time.Time) (st State, err error) {
	// A canceled startup must still acquire the lock to publish its terminal
	// output; RunJob has not installed its deferred Finish yet.
	err = s.WithState(context.WithoutCancel(ctx), id, func(current *State) error {
		if current.Status != StatusQueued {
			return fmt.Errorf("job %s is %s, expected queued", id, current.Status)
		}
		if current.CancelRequested || ctx.Err() != nil {
			current.CancelRequested = true
			e := s.fallbackLocked(current, StatusCanceled, "任务已取消（尚未启动）")
			st = *current
			return errors.Join(context.Canceled, e)
		}
		identity, e := procx.Inspect(os.Getpid())
		if e != nil {
			return e
		}
		if current.Worker != nil && *current.Worker != identity {
			return fmt.Errorf("job %s belongs to another worker", id)
		}
		current.PID = identity.PID
		current.Worker = &identity
		current.Status = StatusRunning
		current.UpdatedAt = now.UTC()
		if e := s.Save(*current); e != nil {
			return e
		}
		st = *current
		return nil
	})
	return
}

// StartProcess holds the lifecycle lock across spawn and durable registration.
// Cancellation cannot slip between checking the job and starting its harness.
func (s Store) StartProcess(ctx context.Context, id string, cmd *exec.Cmd) error {
	return s.WithState(ctx, id, func(st *State) error {
		if st.CancelRequested || Terminal(st.Status) {
			return context.Canceled
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		identity, err := procx.Inspect(cmd.Process.Pid)
		if err == nil {
			st.Processes = append(st.Processes, identity)
			err = s.Save(*st)
		}
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("register harness process: %w", err)
		}
		return nil
	})
}

func atomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".result-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// finishLocked publishes the complete result before the terminal state. Once a
// terminal state exists neither its result nor its status can be replaced.
func (s Store) finishLocked(st *State, result *Result, reason string, finished time.Time) error {
	if Terminal(st.Status) {
		return nil
	}
	if result.WorktreePath == "" {
		result.describeWorktree(*st)
	}
	if st.Worktree != nil && !st.Worktree.Discarded {
		if change, err := worktreeChanges(st.Worktree, false); err == nil {
			result.Diff = change.diff
		} else {
			result.Warnings = append(result.Warnings, "worktree diff 统计失败："+err.Error())
			if result.Status == StatusSucceeded {
				result.Status = StatusFailed
				reason = err.Error()
			}
		}
	}
	if err := atomicJSON(s.ResultPath(st.ID), result); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	st.Status = result.Status
	st.Error = reason
	st.UpdatedAt = finished.UTC()
	st.FinishedAt = st.UpdatedAt
	return s.Save(*st)
}
func (s Store) Finish(id string, result *Result, reason string, finished time.Time) error {
	return s.WithState(context.Background(), id, func(st *State) error {
		if Terminal(st.Status) {
			data, err := os.ReadFile(s.ResultPath(id))
			if err != nil {
				return err
			}
			return json.Unmarshal(data, result)
		}
		if st.CancelRequested {
			result.Status = StatusCanceled
		}
		return s.finishLocked(st, result, reason, finished)
	})
}
func (s Store) fallbackLocked(st *State, status Status, reason string) error {
	if st.CancelRequested && status != StatusCanceled {
		status = StatusCanceled
		reason = "任务已取消（worker 已退出）"
		if st.Status == StatusQueued {
			reason = "任务已取消（尚未启动）"
		}
	}
	r := Result{JobID: st.ID, Status: status, Harness: st.Request.Harness, Access: st.Request.Access, ModelRequested: st.Request.Model, LogPath: s.LogPath(st.ID), Warnings: []string{reason}}
	if h, ok := Lookup(st.Request.Harness); ok && h.Experimental != "" {
		r.Warnings = append([]string{h.Experimental}, r.Warnings...)
	}
	return s.finishLocked(st, &r, reason, time.Now())
}

func (s Store) RequestCancel(id string) (st State, err error) {
	err = s.WithState(context.Background(), id, func(current *State) error {
		if !Terminal(current.Status) {
			if current.Worker != nil {
				alive, e := current.Worker.Alive()
				if e != nil {
					return e
				}
				if !alive {
					// Cleanup still runs when publishing the failure itself fails.
					e := errors.Join(s.fallbackLocked(current, StatusFailed, "job worker is absent or its process identity no longer matches"), current.KillHarnesses())
					st = *current
					return e
				}
			} else if current.Status == StatusRunning {
				e := errors.Join(s.fallbackLocked(current, StatusFailed, "job worker has no verifiable process identity"), current.KillHarnesses())
				st = *current
				return e
			}
			current.CancelRequested = true
			current.UpdatedAt = time.Now().UTC()
			if current.Status == StatusQueued && current.Worker == nil {
				if e := s.fallbackLocked(current, StatusCanceled, "任务已取消（尚未启动）"); e != nil {
					return e
				}
			} else if e := s.Save(*current); e != nil {
				return e
			}
		}
		st = *current
		return nil
	})
	return
}

// Recover detects a dead or replaced worker, including failures to persist running
// or terminal state. A new, unlaunched job gets a short startup window.
func (s Store) Recover(id string) (st State, err error) {
	err = s.WithState(context.Background(), id, func(current *State) error {
		if !Terminal(current.Status) {
			alive := false
			var e error
			if current.Worker != nil {
				alive, e = current.Worker.Alive()
				if e != nil {
					return e
				}
			}
			pendingSince := current.CreatedAt
			if current.Worktree != nil && current.UpdatedAt.After(pendingSince) {
				// Worktree checkout holds this lock; its completion starts the
				// same short launch window that ordinary jobs get at creation.
				pendingSince = current.UpdatedAt
			}
			pending := current.Status == StatusQueued && current.PID == 0 && current.Worker == nil && time.Since(pendingSince) < 5*time.Second
			if !alive && !pending {
				e := errors.Join(s.fallbackLocked(current, StatusFailed, "job worker is absent or its process identity no longer matches"), current.KillHarnesses())
				st = *current
				return e
			}
		}
		st = *current
		return nil
	})
	return
}

// KillHarnesses reaps the independent groups registered before protocol startup.
func (st State) KillHarnesses() error {
	var cleanupErr error
	for _, p := range st.Processes {
		if err := p.Signal(syscall.SIGKILL, true); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("kill harness %d: %w", p.PID, err))
		}
	}
	return cleanupErr
}
