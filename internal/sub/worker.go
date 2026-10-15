package sub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/orchestrate"
	"github.com/agentjoey/pactify/internal/procx"
)

const (
	sessionHidingFailed    = "Session hiding failed"
	sessionArchivingFailed = "Session archiving failed"
)

type WorkerConfig struct {
	ToolCallLimit int // 0 disables the limit; CLI default is 40.
	IdleTimeout   time.Duration
	Now           func() time.Time
}

// Serialize backend stderr with event records in the shared log.
type jobLog struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *jobLog) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.w.Write(p) }

// RunJob owns a single job from its queued state through durable terminal output.
func RunJob(ctx context.Context, store Store, id string, h Harness, cfg WorkerConfig) (result Result, runErr error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 10 * time.Minute
	}
	started := cfg.Now()
	st, err := store.claim(ctx, id, started)
	if err != nil {
		return result, err
	}

	req := st.Request
	if st.Worktree != nil {
		req.Cwd = st.Worktree.Cwd
	}
	result = Result{JobID: id, Harness: req.Harness, ModelRequested: req.Model, Access: req.Access, Status: StatusFailed, LogPath: store.LogPath(id)}
	if h.Experimental != "" {
		result.Warnings = append(result.Warnings, h.Experimental)
	}
	result.describeWorktree(st)
	unverifiedWarning := ""
	// Persist the result before the terminal state, so waiters never observe a partial result.
	defer func() {
		p := recover()
		if p != nil {
			runErr = fmt.Errorf("worker panic: %v", p)
			result.Status = StatusFailed
			result.Warnings = append(result.Warnings, runErr.Error())
		}
		finished := cfg.Now()
		elapsed := finished.Sub(started)
		if elapsed < 0 {
			elapsed = 0
		}
		secs := int(elapsed.Seconds())
		result.Duration = fmt.Sprintf("%dm%02ds", secs/60, secs%60)
		reason := ""
		if runErr != nil {
			reason = runErr.Error()
		} else {
			for _, warning := range result.Warnings {
				// These warnings describe harness readiness, not a job error.
				if warning != h.Experimental && warning != unverifiedWarning && warning != dirtyWorktreeWarning {
					reason = warning
					break
				}
			}
		}
		if e := store.Finish(id, &result, reason, finished); e != nil {
			runErr = fmt.Errorf("save terminal output: %w", e)
		}
		if p != nil {
			panic(p)
		} // subWorker records the stack; the result already has the cause.
	}()
	fail := func(err error) (Result, error) {
		result.Warnings = append(result.Warnings, err.Error())
		if ctx.Err() != nil {
			result.Status = StatusCanceled
		}
		return result, err
	}
	if req.Access != "write" && req.Access != "read-only" {
		return fail(fmt.Errorf("invalid access %q", req.Access))
	}
	var snap Snapshot
	if st.Worktree == nil {
		snap, err = TakeSnapshot(req.Cwd)
		if err != nil {
			return fail(err)
		}
	}
	if h.Version != nil {
		result.CLIVersion, _ = h.Version(ctx)
	}
	if result.CLIVersion != "" {
		if previous := store.lastCLIVersionFor(req.Harness); previous != "" && previous != result.CLIVersion {
			result.Warnings = append(result.Warnings, fmt.Sprintf("CLI version changed: %s → %s", previous, result.CLIVersion))
		}
	}
	unverifiedWarning = unverifiedVersionWarning(filepath.Dir(store.Root), h, result.CLIVersion)
	if unverifiedWarning != "" {
		result.Warnings = append(result.Warnings, unverifiedWarning)
	}
	file, err := os.OpenFile(store.LogPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fail(err)
	}
	defer file.Close()
	log := &jobLog{w: file}
	encoder := json.NewEncoder(log)
	opts := cockpit.StartOpts{RepoDir: req.Cwd, Model: req.Model, Effort: req.Effort, Access: cockpit.Access(req.Access), Env: orchestrate.CrossVendorStrip(h.Kind), Stderr: log, Hidden: true}
	opts.Env = append(opts.Env, "PACTIFY_HOME="+filepath.Dir(store.Root))
	// Startup follows cancellation. Once started, Interrupt gets a grace period before Close.
	sessionCtx, stopSession := context.WithCancel(context.WithoutCancel(ctx))
	sessionCtx = procx.WithLauncher(sessionCtx, func(cmd *exec.Cmd) error { return store.StartProcess(ctx, id, cmd) })
	defer stopSession()
	stopStartup := context.AfterFunc(ctx, stopSession)
	backend := h.Backend()
	var sess cockpit.Session
	if req.Resume != "" {
		if b, ok := backend.(cockpit.OptsResumer); ok {
			sess, err = b.ResumeWith(sessionCtx, req.Resume, opts)
		} else {
			sess, err = backend.Resume(sessionCtx, req.Resume)
		}
	} else {
		sess, err = backend.Start(sessionCtx, opts)
	}
	stopStartup()
	if err != nil {
		var identified interface{ SessionID() string }
		if errors.As(err, &identified) {
			result.SessionID = identified.SessionID()
			if h.Conceal != nil && result.SessionID != "" {
				if e := h.Conceal(ConcealContext{SessionID: result.SessionID, Cwd: req.Cwd}); e != nil {
					result.Warnings = append(result.Warnings, sessionHidingFailed+"; it will remain in the harness session list: "+e.Error())
				}
			}
		}
		return fail(err)
	}
	defer sess.Close()
	result.SessionID = sess.ThreadID()
	workCtx, stopWork := context.WithCancel(ctx)
	defer stopWork()
	approvalErr := make(chan error, 1)
	go func() {
		approvals := sess.Approvals()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case a, ok := <-approvals:
				if !ok {
					return
				}
				decision := cockpit.DecisionAllow
				latest, stateErr := store.Load(id)
				if req.Access == "read-only" || ctx.Err() != nil || stateErr != nil || latest.CancelRequested {
					decision = cockpit.DecisionDeny
				}
				if a.Respond != nil {
					if e := a.Respond(decision); e != nil {
						select {
						case approvalErr <- e:
						case <-workCtx.Done():
						}
						return
					}
				}
			}
		}
	}()
	promptErr := make(chan error, 1)
	go func() {
		if err := workCtx.Err(); err != nil {
			promptErr <- err
			return
		}
		promptErr <- sess.Prompt(workCtx, cockpit.UserMessage{Text: req.Brief})
	}()
	var collector Collector
	lastActivity := cfg.Now()
	fingerprint := gitx.TreeFingerprint(req.Cwd)
	interval := cfg.IdleTimeout / 4
	if interval > time.Second {
		interval = time.Second
	}
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var deadline <-chan time.Time
	var grace *time.Timer
	defer func() {
		if grace != nil {
			grace.Stop()
		}
	}()
	interrupted := false
	interruptDone := make(chan struct{})
	graceExpired := false
	canceled := false
	turnFailed := false
	interrupt := func(warning string) {
		if interrupted {
			return
		}
		interrupted = true
		result.Warnings = append(result.Warnings, warning)
		grace = time.NewTimer(10 * time.Second)
		deadline = grace.C
		go func() {
			defer close(interruptDone)
			c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = sess.Interrupt(c)
		}()
	}
	cancelCh := ctx.Done()
	events := sess.Events()
loop:
	for !collector.TurnDone {
		select {
		case <-cancelCh:
			canceled = true
			cancelCh = nil
			interrupt("Task canceled")
		case <-deadline:
			graceExpired = true
			break loop
		case e := <-approvalErr:
			interrupt("Approval failed: " + e.Error())
		case e := <-promptErr:
			promptErr = nil
			if e != nil {
				interrupt(e.Error())
			}
		case event, ok := <-events:
			if !ok {
				result.Warnings = append(result.Warnings, "Session closed before the turn ended")
				break loop
			}
			if e := encoder.Encode(event); e != nil {
				interrupt("Failed to write log: " + e.Error())
			}
			lastActivity = cfg.Now()
			collector.Add(event)
			if req.Access == "read-only" && cfg.ToolCallLimit > 0 && collector.ToolCalls > cfg.ToolCallLimit {
				interrupt(fmt.Sprintf("Tool-call limit exceeded (%d calls)", collector.ToolCalls))
			}
			if event.Kind == cockpit.EventState && event.State == cockpit.StateTurnFailed {
				turnFailed = true
				message := event.Err
				if message == "" {
					message = "Turn failed"
				}
				result.Warnings = append(result.Warnings, message)
			}
		case <-ticker.C:
			latest, e := store.Load(id)
			if e != nil {
				interrupt("Failed to read job state: " + e.Error())
			} else if latest.CancelRequested && !canceled {
				canceled = true
				stopWork()
				interrupt("Task canceled")
			}
			current := gitx.TreeFingerprint(req.Cwd)
			if current != fingerprint {
				fingerprint = current
				lastActivity = cfg.Now()
			}
			if cfg.Now().Sub(lastActivity) >= cfg.IdleTimeout {
				interrupt("Idle timeout")
			}
		}
	}
	if ctx.Err() != nil {
		canceled = true
		interrupt("Task canceled")
	}
	// Prompt cancellation can generate a local terminal event before the remote
	// turn ends. Let Interrupt reach the harness before teardown, within its grace.
	if canceled && !graceExpired {
		select {
		case <-interruptDone:
		case <-deadline:
		}
	}
	if !collector.TurnDone {
		stopWork()
	}
	// Close now (the deferred Close is then a no-op) so a hidden session's
	// archive failure can still reach the summary.
	if err := sess.Close(); errors.Is(err, cockpit.ErrArchive) {
		result.Warnings = append(result.Warnings, sessionArchivingFailed+"; it will remain in the harness session list: "+err.Error())
	}
	info := cockpit.SessionInfo{}
	if s, ok := sess.(cockpit.InfoSession); ok {
		info = s.Info()
	}
	result.ModelActual = info.Model
	result.InputTokens = collector.Usage.InputTokens
	result.OutputTokens = collector.Usage.OutputTokens
	result.CostUSD = collector.Usage.CostUSD
	result.ToolCalls = collector.ToolCalls
	result.RateLimit = SummarizeRateLimit(collector.RateLimit)
	if h.Verify != nil {
		model, in, out, ok := h.Verify(sess.ThreadID(), info)
		result.ModelVerified = ok
		if ok {
			result.ModelActual = model
			if h.Kind == "kimi-cli" {
				result.InputTokens = in
				result.OutputTokens = out
			}
		}
	}
	if h.Conceal != nil {
		if err := h.Conceal(ConcealContext{SessionID: sess.ThreadID(), Cwd: req.Cwd}); err != nil {
			result.Warnings = append(result.Warnings, sessionHidingFailed+"; it will remain in the harness session list: "+err.Error())
		}
	}
	if req.Model != "" && result.ModelActual != "" && req.Model != result.ModelActual {
		result.Warnings = append(result.Warnings, "Actual model does not match the requested model")
	}
	result.FinalText, result.Truncated = TruncateFinal(collector.FinalText())
	if st.Worktree == nil {
		result.Diff, err = DiffSince(req.Cwd, snap)
	} // Worktree jobs publish the full final-tree diff in Finish, including failures.
	if err != nil {
		result.Warnings = append(result.Warnings, err.Error())
	}
	if canceled || ctx.Err() != nil {
		result.Status = StatusCanceled
	} else if err == nil && collector.TurnDone && !turnFailed && !interrupted {
		result.Status = StatusSucceeded
	}
	return result, nil
}
