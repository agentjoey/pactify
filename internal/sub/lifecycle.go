package sub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
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
			e := s.fallbackLocked(current, StatusCanceled, "Task canceled (not yet started)")
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
		current.StartedAt = now.UTC()
		current.UpdatedAt = now.UTC()
		if e := s.Save(*current); e != nil {
			return e
		}
		st = *current
		return nil
	})
	return
}

// inspectHarness is procx.Inspect; tests replace it to simulate a harness
// that exits before it is registered.
var inspectHarness = procx.Inspect

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
		identity, err := inspectHarness(cmd.Process.Pid)
		if errors.Is(err, syscall.ESRCH) {
			// The harness already exited (a zombie or gone): there is nothing
			// to track or signal, and failing here would hide the harness's own
			// error behind "register harness process". Let the run report it.
			return nil
		}
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
	result.Sandboxed = st.Request.Harness == "codex"
	result.ConfigMode = st.EffectiveConfigMode()
	result.FallbackFrom, result.FallbackTo = st.FallbackFrom, st.FallbackTo
	// Preserve routing evidence for every terminal path, including cancellation,
	// launch failures and worker recovery, without duplicating worker warnings.
	for _, warning := range st.RouteWarnings {
		if !slices.Contains(result.Warnings, warning) {
			result.Warnings = append(result.Warnings, warning)
		}
	}
	if result.WorktreePath == "" {
		result.describeWorktree(*st)
	}
	if st.Worktree != nil && !st.Worktree.Discarded {
		warnings, err := s.collectWorktreeWarnings(st)
		result.Warnings = append(result.Warnings, warnings...)
		if err != nil {
			result.Warnings = append(result.Warnings, "Failed to inspect worktree HEAD and branches: "+err.Error())
		}
		if change, err := worktreeChanges(st.Worktree, false); err == nil {
			result.Diff = change.diff
		} else {
			result.Warnings = append(result.Warnings, "Failed to collect worktree diff stats: "+err.Error())
			if result.Status == StatusSucceeded {
				result.Status = StatusFailed
				reason = err.Error()
			}
		}
	}
	if verification, violation := s.verifyReadOnly(st, result); verification != "" {
		result.Status = StatusFailed
		result.FailureClass, result.QuotaResetAt = "other", nil
		if violation {
			result.FailureClass = "readonly_violation"
		}
		reason = verification
		result.Warnings = append(result.Warnings, verification)
	}

	if result.Status == StatusSucceeded {
		result.FailureClass, result.QuotaResetAt = "", nil
	} else if Terminal(result.Status) {
		switch {
		case result.FailureClass == "readonly_violation":
		case result.Status == StatusCanceled || st.CancelRequested:
			result.FailureClass, result.QuotaResetAt = "canceled", nil
		case reason == "Idle timeout":
			result.FailureClass, result.QuotaResetAt = "idle_timeout", nil
		case result.FailureClass == "" || result.FailureClass == "other":
			f := cockpit.ClassifyFailure(st.Request.Harness, reason, nil, finished)
			result.FailureClass, result.QuotaResetAt = f.Class, f.ResetAt
		}
		if result.FailureClass != "quota" {
			result.QuotaResetAt = nil
		}
		if result.FailureClass == "quota" {
			message := result.failureMessage
			if message == "" {
				message = reason
			}
			model := result.ModelActual
			if model == "" {
				model = st.Request.Model
			}
			if err := s.rememberQuota(QuotaRoute{Harness: st.Request.Harness, Model: model, ResetAt: result.QuotaResetAt, JobID: st.ID, RecordedAt: finished.UTC(), Message: message}); err != nil {
				result.Warnings = append(result.Warnings, "Failed to record quota memory: "+err.Error())
			}
		}
	}
	if err := s.quotaFallbackLocked(st, result, finished); err != nil {
		result.Warnings = append(result.Warnings, "Automatic fallback failed: "+err.Error())
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
	initial, err := s.Load(id)
	if err != nil {
		return err
	}
	release, err := s.worktreeLease(initial)
	if err != nil {
		return err
	}
	defer release()
	return s.WithState(context.Background(), id, func(st *State) error {
		if Terminal(st.Status) {
			data, err := os.ReadFile(s.ResultPath(id))
			if err != nil {
				return err
			}
			return json.Unmarshal(data, result)
		}
		cleanupErr := st.KillHarnesses()
		if cleanupErr == nil && st.TempDirOwned {
			cleanupErr = s.cleanupJobTemp(id)
		}
		if err := cleanupErr; err != nil {
			result.fallbackBlocked = true
			warning := "Job temp cleanup failed: " + err.Error()
			if !slices.Contains(result.Warnings, warning) {
				result.Warnings = append(result.Warnings, warning)
			}
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
		reason = "Task canceled (worker exited)"
		if st.Status == StatusQueued {
			reason = "Task canceled (not yet started)"
		}
	}
	r := Result{Retries: st.Retries, JobID: st.ID, Status: status, Harness: st.Request.Harness, Access: st.Request.Access, ModelRequested: st.Request.Model, LogPath: s.LogPath(st.ID), Warnings: []string{reason}}
	if st.Status == StatusRunning {
		c := collectLoggedUsage(r.LogPath)
		r.InputTokens, r.OutputTokens, r.CostUSD = c.Usage.InputTokens, c.Usage.OutputTokens, c.Usage.CostUSD
		if st.Request.Harness == "opencode" && !c.JobUsageDelta {
			// Legacy ACP usage_update carries session cost, including jobs before a resume.
			// The lost worker's export baseline cannot be reconstructed from logs.
			r.CostUSD = 0
		}
		r.ToolCalls = c.ToolCalls
		r.markUsagePartial()
	}
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
					e := s.failStoppedWorkerLocked(current, "job worker is absent or its process identity no longer matches")
					st = *current
					return e
				}
			} else if current.Status == StatusRunning {
				e := s.failStoppedWorkerLocked(current, "job worker has no verifiable process identity")
				st = *current
				return e
			}
			current.CancelRequested = true
			current.UpdatedAt = time.Now().UTC()
			if current.Status == StatusQueued && current.Worker == nil {
				if e := s.fallbackLocked(current, StatusCanceled, "Task canceled (not yet started)"); e != nil {
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
				e := s.failStoppedWorkerLocked(current, "job worker is absent or its process identity no longer matches")
				st = *current
				return e
			}
		}
		st = *current
		return nil
	})
	return
}

// Stop registered harnesses before publishing fallback output and deleting their
// temp files. Still publish a failure when process cleanup itself fails, but keep
// the temp directory in that case so a surviving process cannot recreate it.
func (s Store) failStoppedWorkerLocked(st *State, reason string) error {
	killErr := st.KillHarnesses()
	var cleanupErr error
	if killErr == nil && st.TempDirOwned {
		cleanupErr = s.cleanupJobTemp(st.ID)
	}
	finishErr := s.fallbackLocked(st, StatusFailed, reason)
	return errors.Join(killErr, cleanupErr, finishErr)
}

// KillHarnesses combines durable snapshots with a last snapshot before stopping
// leaders, so detached groups are still owned after their ancestry disappears.
func (st State) KillHarnesses() error {
	var cleanupErr error
	if st.ProcessTrackingError != "" {
		cleanupErr = errors.New(st.ProcessTrackingError)
	}
	descendants := append([]procx.Identity(nil), st.Descendants...)
	for _, p := range append(append([]procx.Identity(nil), st.Processes...), st.Descendants...) {
		found, err := procx.Descendants(p)
		cleanupErr = errors.Join(cleanupErr, err)
		for _, child := range found {
			if !slices.Contains(descendants, child) {
				descendants = append(descendants, child)
			}
		}
	}
	for _, p := range st.Processes {
		if err := p.Signal(syscall.SIGKILL, true); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("kill harness %d: %w", p.PID, err))
		}
	}
	cleanupErr = errors.Join(cleanupErr, procx.TerminateDescendants("job", descendants, 200*time.Millisecond))
	remaining, err := procx.WaitDescendants("harness", st.Processes, 200*time.Millisecond)
	cleanupErr = errors.Join(cleanupErr, err)
	if len(remaining) > 0 {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("%d harness processes still alive", len(remaining)))
	}
	// A short-lived parent can exit between tracking ticks. Its detached tools
	// still carry the job marker even though no ancestry snapshot recorded them.
	marker := st.ProcessMarker
	if marker == "" {
		marker = st.ID
	}
	marked, err := procx.FindJobProcesses(marker)
	cleanupErr = errors.Join(cleanupErr, err, procx.TerminateDescendants("job", marked, 200*time.Millisecond))
	return cleanupErr
}

// snapshotDescendants retains live identities even after they are reparented.
// An inspection failure is latched: subsequent snapshots cannot prove that no
// process escaped during that gap, so temp cleanup must remain conservative.
func (s Store) snapshotDescendants(id string) error {
	return s.WithState(context.Background(), id, func(st *State) error {
		if Terminal(st.Status) {
			return nil
		}
		var descendants []procx.Identity
		var snapshotErr error
		for _, p := range st.Descendants {
			alive, err := p.Alive()
			snapshotErr = errors.Join(snapshotErr, err)
			if alive || err != nil {
				descendants = append(descendants, p)
			}
		}
		roots := append(append([]procx.Identity(nil), st.Processes...), descendants...)
		seen := map[procx.Identity]bool{}
		for _, p := range roots {
			if seen[p] {
				continue
			}
			seen[p] = true
			found, err := procx.Descendants(p)
			snapshotErr = errors.Join(snapshotErr, err)
			for _, child := range found {
				seen[child] = true
				if !slices.Contains(descendants, child) {
					descendants = append(descendants, child)
				}
			}
		}
		if snapshotErr != nil && st.ProcessTrackingError == "" {
			st.ProcessTrackingError = snapshotErr.Error()
		}
		if slices.Equal(st.Descendants, descendants) && snapshotErr == nil {
			return nil
		}
		st.Descendants = descendants
		return errors.Join(snapshotErr, s.Save(*st))
	})
}

func (s Store) trackProcesses(id string) func() error {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		var trackErr error
		for {
			if err := s.snapshotDescendants(id); err != nil && trackErr == nil {
				trackErr = err
			}
			select {
			case <-ctx.Done():
				if trackErr != nil {
					// Include persistence failures, not just inspection failures, in the
					// durable cleanup decision. Do not retain one error per polling tick.
					err := s.WithState(context.Background(), id, func(st *State) error {
						if st.ProcessTrackingError != "" {
							return nil
						}
						st.ProcessTrackingError = trackErr.Error()
						return s.Save(*st)
					})
					trackErr = errors.Join(trackErr, err)
				}
				done <- trackErr
				return
			case <-ticker.C:
			}
		}
	}()
	return func() error { cancel(); return <-done }
}
