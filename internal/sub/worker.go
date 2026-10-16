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
	"slices"
	"sync"
	"sync/atomic"
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
	RetryBackoff    func(int) time.Duration // 1-based attempt; nil uses 30s then 120s.
	CacheHome       string                  // Optional shared cache home; doctor keeps job storage disposable.
	ToolCallLimit   int                     // 0 disables the limit; CLI default is 40.
	IdleTimeout     time.Duration
	KimiWireTimeout time.Duration // Default 10s to flush per-job wire usage before Close.
	Now             func() time.Time
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
	if cfg.KimiWireTimeout <= 0 {
		cfg.KimiWireTimeout = 10 * time.Second
	}
	started := cfg.Now()
	st, err := store.claim(ctx, id, started)
	if err != nil {
		return result, err
	}

	req := st.Request
	retryLimit := DefaultRetries
	if req.Retries != nil {
		retryLimit = *req.Retries
	}
	if cfg.RetryBackoff == nil {
		cfg.RetryBackoff = retryBackoff
	}
	if st.Worktree != nil {
		req.Cwd = st.Worktree.Cwd
	}
	result = Result{JobID: id, Harness: req.Harness, ModelRequested: req.Model, Access: req.Access, Status: StatusFailed, LogPath: store.LogPath(id)}
	result.ConfigMode = st.EffectiveConfigMode()
	result.Warnings = append(result.Warnings, st.RouteWarnings...)
	if h.Experimental != "" {
		result.Warnings = append(result.Warnings, h.Experimental)
	}
	result.describeWorktree(st)
	unverifiedWarning, versionWarning := "", ""
	var cacheWarnings []string
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
		} else if len(result.Retries) > 0 && result.Status != StatusSucceeded && result.failureMessage != "" {
			reason = result.failureMessage
		} else {
			for _, warning := range result.Warnings {
				// These warnings describe harness readiness, not a job error.
				if !slices.Contains(cacheWarnings, warning) && !slices.Contains(st.RouteWarnings, warning) && warning != h.Experimental && warning != unverifiedWarning && warning != versionWarning && warning != dirtyWorktreeWarning {
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
		f := cockpit.FailureFromError(req.Harness, err, cfg.Now())
		result.FailureClass, result.QuotaResetAt = f.Class, f.ResetAt
		if ctx.Err() != nil {
			result.Status = StatusCanceled
		}
		return result, err
	}
	if retryLimit < 0 || retryLimit > MaxRetries {
		return fail(fmt.Errorf("retries must be between 0 and %d", MaxRetries))
	}
	if req.Access != "write" && req.Access != "read-only" {
		return fail(fmt.Errorf("invalid access %q", req.Access))
	}
	var snap Snapshot
	if st.Worktree == nil {
		if st.Baseline != nil {
			snap = *st.Baseline
		} else {
			snap, err = TakeSnapshot(req.Cwd)
		}
		if err != nil {
			if !snap.EmptyTree {
				return fail(err)
			}
			result.Warnings = append(result.Warnings, "Changes unavailable: cannot capture baseline: "+err.Error())
		}
	}
	if snap.Base != "" && err == nil && st.Baseline == nil && st.Routing != nil {
		if e := store.WithState(ctx, id, func(current *State) error { current.Baseline = &snap; return store.Save(*current) }); e != nil {
			return fail(e)
		}
	}
	if h.Version != nil {
		result.CLIVersion, _ = h.Version(ctx)
	}
	if result.CLIVersion != "" {
		if previous := store.lastCLIVersionFor(req.Harness); previous != "" && previous != result.CLIVersion {
			versionWarning = fmt.Sprintf("CLI version changed: %s → %s", previous, result.CLIVersion)
			result.Warnings = append(result.Warnings, versionWarning)
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
	opts.MinimalConfig = result.ConfigMode == "minimal"
	opts.Env = append(opts.Env, "PACTIFY_HOME="+filepath.Dir(store.Root), "PACTIFY_JOB="+id)
	cacheHome := cfg.CacheHome
	if cacheHome == "" {
		cacheHome = filepath.Dir(store.Root)
	}
	cacheWarnings, err = prepareJobCaches(cacheHome, h.Name == "codex", &opts)
	result.Warnings = append(result.Warnings, cacheWarnings...)
	if err != nil {
		return fail(err)
	}
	var layouts []*managedHome
	// Release home leases only after process cleanup, including detached tools.
	defer func() {
		for _, layout := range layouts {
			result.Warnings = append(result.Warnings, layout.Cleanup()...)
		}
	}()
	if err := store.prepareJobTemp(ctx, id); err != nil {
		return fail(err)
	}
	stopTracking := store.trackProcesses(id)
	defer func() {
		trackErr := stopTracking()
		current, loadErr := store.Load(id)
		cleanupErr := errors.Join(trackErr, loadErr)
		if loadErr == nil {
			cleanupErr = errors.Join(cleanupErr, current.KillHarnesses())
		}
		if cleanupErr == nil {
			cleanupErr = store.cleanupJobTemp(id)
		}
		if err := cleanupErr; err != nil {
			result.Warnings = append(result.Warnings, "Job temp cleanup failed: "+err.Error())
		}
	}()
	tmpDir, err := filepath.Abs(filepath.Join(store.Dir(id), "tmp"))
	if err != nil {
		return fail(err)
	}
	opts.Env = append(opts.Env, "TMPDIR="+tmpDir)
	if h.Name == "codex" && opts.Access == cockpit.AccessWrite {
		opts.WritableRoots = append(opts.WritableRoots, tmpDir)
	}
	harnessHome := st.HarnessHome

	minimalHome := h.Home != nil && opts.MinimalConfig
	if h.Home != nil {
		var env []string
		var homes map[string]string
		harnessHome, homes, env, layouts, err = prepareHarnessHomes(ctx, h.Home, harnessHome, st.HarnessHomes, minimalHome)

		if err != nil {
			return fail(err)
		}
		if e := store.WithState(ctx, id, func(current *State) error {
			current.HarnessHome = harnessHome
			current.HarnessHomes = homes
			return store.Save(*current)
		}); e != nil {
			return fail(e)
		}
		opts.Env = append(opts.Env, env...)
	}
	if h.Name == "opencode" {
		opts.Env = append(opts.Env, opencodeVendorStrip()...)
		opts.Env, err = cockpit.OpencodeEnv(opts)
		if err != nil {
			return fail(err)
		}
	}

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
			if h.Conceal != nil && !minimalHome && result.SessionID != "" {
				if e := h.Conceal(ConcealContext{SessionID: result.SessionID, Cwd: req.Cwd, Home: harnessHome}); e != nil {
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
	wireOffset := int64(0)
	if h.LocalUsageBaseline != nil && h.Verify != nil && req.Resume != "" {
		var baselineErr error
		wireOffset, baselineErr = h.LocalUsageBaseline(harnessHome, sess.ThreadID())
		if baselineErr != nil {
			wireOffset = -1 // Never count historical usage when the baseline is unknown.
			result.Warnings = append(result.Warnings, "Kimi wire baseline unavailable; tokens may be incomplete: "+baselineErr.Error())
		}
	}
	if h.VerificationBaseline != nil && h.Verify != nil && req.Resume != "" {
		wireOffset, err = h.VerificationBaseline(ctx, VerificationInput{SessionID: sess.ThreadID(), Env: opts.Env, Cwd: req.Cwd})
		if err != nil {
			wireOffset = -1
			result.Warnings = append(result.Warnings, "opencode export baseline unavailable: "+err.Error())
		}
	}
	var prompts atomic.Int32
	startPrompt := func(text string) chan error {
		ch := make(chan error, 1)
		go func() {
			if err := workCtx.Err(); err != nil {
				ch <- err
				return
			}
			prompts.Add(1)
			ch <- sess.Prompt(workCtx, cockpit.UserMessage{Text: text})
		}()
		return ch
	}
	promptErr := startPrompt(req.Brief)
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
	promptFailed := false
	interrupt := func(warning string) {
		if interrupted {
			return
		}
		interrupted = true
		if len(result.Retries) > 0 {
			result.failureMessage = warning
		}
		if warning == "Idle timeout" {
			result.FailureClass, result.QuotaResetAt = "idle_timeout", nil
		} else if warning != "Task canceled" {
			f := cockpit.ClassifyFailure(req.Harness, warning, nil, cfg.Now())
			result.FailureClass, result.QuotaResetAt = f.Class, f.ResetAt
		}
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
	sessionClosed := false
	recordEvent := func(event cockpit.Event) {
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
	}

turns:
	for {
	loop:
		for !collector.TurnDone || promptErr != nil {
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
				if e != nil && !interrupted {
					promptFailed = true
					result.failureMessage = e.Error()
					f := cockpit.FailureFromError(req.Harness, e, cfg.Now())
					// ACP publishes its terminal event before returning the RPC
					// error. Drain it before deciding whether interruption is needed.
				drain:
					for {
						select {
						case event, ok := <-events:
							if !ok {
								sessionClosed = true
								break drain
							}
							recordEvent(event)
						default:
							break drain
						}
					}
					if !collector.TurnDone && f.Class != "transient" {
						interrupt(e.Error())
					} else if !collector.TurnDone {
						recordEvent(cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: e.Error()})
					} else if !slices.Contains(result.Warnings, e.Error()) {
						result.Warnings = append(result.Warnings, e.Error())
					}
					result.FailureClass, result.QuotaResetAt = f.Class, f.ResetAt
				}
			case event, ok := <-events:
				if !ok {
					sessionClosed = true
					result.Warnings = append(result.Warnings, "Session closed before the turn ended")
					break loop
				}
				recordEvent(event)
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
		if turnFailed || promptFailed {
			result.classifyTurnFailure(req.Harness, collector, cfg.Now(), promptFailed)
		}
		if result.FailureClass != "transient" || canceled || ctx.Err() != nil || interrupted || !collector.TurnDone || sessionClosed || graceExpired || len(result.Retries) >= retryLimit {
			break turns
		}
		latest, stateErr := store.Load(id)
		if stateErr != nil {
			interrupt("Failed to read job state: " + stateErr.Error())
			break turns
		}
		if latest.CancelRequested {
			canceled = true
			break turns
		}
		if !h.TransientRetry || sess.ThreadID() == "" {
			result.Warnings = append(result.Warnings, "transient retry unsupported for "+req.Harness)
			break turns
		}
		// A failed request may omit its final usage even if the continuation succeeds.
		result.markUsagePartial()
		attempt := len(result.Retries) + 1
		if err := store.retryProgress(ctx, id, attempt, retryLimit, nil, cfg.Now()); err != nil {
			if errors.Is(err, context.Canceled) {
				canceled = true
			} else {
				interrupt(err.Error())
			}
			break turns
		}
		if err := store.waitRetry(ctx, id, cfg.RetryBackoff(attempt)); err != nil {
			if errors.Is(err, context.Canceled) {
				canceled = true
			} else {
				interrupt(err.Error())
			}
			break turns
		}
		retry := Retry{Class: "transient", Error: result.failureMessage, At: cfg.Now().UTC()}
		if err := store.retryProgress(ctx, id, attempt, retryLimit, &retry, cfg.Now()); err != nil {
			if errors.Is(err, context.Canceled) {
				canceled = true
			} else {
				interrupt(err.Error())
			}
			break turns
		}
		result.Retries = append(result.Retries, retry)
		// The prompt call and its terminal event have both finished. Reuse the
		// live session; resetting turn-local output leaves cumulative usage intact.
		turnFailed, promptFailed = false, false
		// Prior errors belong to retry history, not the final turn's failure reason.
		kept := result.Warnings[:0]
		for _, warning := range result.Warnings {
			if warning != collector.FailureMessage && warning != result.failureMessage {
				kept = append(kept, warning)
			}
		}
		result.Warnings = kept
		result.FailureClass, result.QuotaResetAt, result.failureMessage = "", nil, ""
		marker := cockpit.Event{Kind: cockpit.EventSystem, State: "transient_retry", Text: req.Harness}
		if err := encoder.Encode(marker); err != nil {
			interrupt("Failed to write log: " + err.Error())
			break turns
		}
		collector.Add(marker)
		lastActivity = cfg.Now()
		promptErr = startPrompt(retryPrompt)
	}
	if ctx.Err() != nil {
		canceled = true
		interrupt("Task canceled")
	}
	if canceled {
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
	closeSession := func() {
		if err := sess.Close(); errors.Is(err, cockpit.ErrArchive) {
			result.Warnings = append(result.Warnings, sessionArchivingFailed+"; it will remain in the harness session list: "+err.Error())
		}
	}
	// Codex archives on Close. Kimi must flush wire records before process teardown.
	if h.LocalUsageBaseline == nil {
		closeSession()
	}

	info := cockpit.SessionInfo{}
	if s, ok := sess.(cockpit.InfoSession); ok {
		info = s.Info()
	}
	result.SessionID = sess.ThreadID()
	info.Home = harnessHome
	result.ModelActual = info.Model
	result.InputTokens = collector.Usage.InputTokens
	result.OutputTokens = collector.Usage.OutputTokens
	result.CostUSD = collector.Usage.CostUSD
	result.ToolCalls = collector.ToolCalls
	result.RateLimit = SummarizeRateLimit(collector.RateLimit)
	// An abnormal end may omit the last accounting update, even if earlier
	// requests reported usage. Treat retained counters as a lower bound.
	recoverUsage := turnFailed || interrupted || !collector.TurnDone
	if recoverUsage {
		result.markUsagePartial()
	}
	if h.Verify != nil {
		verifyCtx := ctx
		if h.LocalUsageBaseline != nil {
			var cancel context.CancelFunc
			verifyCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), cfg.KimiWireTimeout)
			defer cancel()
		}
		if h.VerificationBaseline != nil {
			var cancel context.CancelFunc
			verifyCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			defer cancel()
		}
		verified := h.Verify(verifyCtx, VerificationInput{SessionID: sess.ThreadID(), Info: info, WireOffset: wireOffset, Prompts: int(prompts.Load()), Env: opts.Env, Cwd: req.Cwd})
		result.ModelVerified = verified.Verified
		if verified.CostUSD != nil {
			result.CostUSD = *verified.CostUSD
		}
		if verified.Verified {
			result.ModelActual = verified.Model
		}
		if h.LocalUsageBaseline != nil || verified.UsageRecorded || verified.InputTokens != 0 || verified.OutputTokens != 0 {
			result.InputTokens = verified.InputTokens
			result.OutputTokens = verified.OutputTokens
		}
		if verified.Warning != "" {
			result.Warnings = append(result.Warnings, verified.Warning)
		}
		if verified.UsagePartial {
			result.markUsagePartial()
		}
	}
	if h.LocalUsageBaseline != nil {
		closeSession()
	}

	if h.Conceal != nil && !minimalHome {
		if err := h.Conceal(ConcealContext{SessionID: sess.ThreadID(), Cwd: req.Cwd, Home: harnessHome}); err != nil {
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
		warning := err.Error()
		if snap.EmptyTree {
			warning = "Changes unavailable or incomplete: " + warning
		}
		result.Warnings = append(result.Warnings, warning)
	}
	if canceled || ctx.Err() != nil {
		result.Status = StatusCanceled
	} else if (err == nil || snap.EmptyTree) && collector.TurnDone && !turnFailed && !interrupted {
		result.Status = StatusSucceeded
	}
	if result.Status == StatusFailed && (!interrupted || promptFailed) {
		result.classifyTurnFailure(req.Harness, collector, cfg.Now(), promptFailed)
	}
	return result, nil
}
