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
	"strings"
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
	ProcessMarker         string                  // Doctor scopes its repeated probe IDs to a disposable store.
	ReadOnlyPolicy        bool                    // Doctor exercises the hook independently of native plan mode.
	RetryBackoff          func(int) time.Duration // 1-based attempt; nil uses 30s then 120s.
	CacheHome             string                  // Optional shared cache home; doctor keeps job storage disposable.
	ToolCallLimit         int                     // 0 disables the limit; CLI default is 40.
	IdleTimeout           time.Duration
	ActiveToolIdleTimeout time.Duration // Default 30m; configurable through PACTIFY_SUB_ACTIVE_TOOL_IDLE_TIMEOUT.
	KimiWireTimeout       time.Duration // Default 10s to flush per-job wire usage before Close.
	Now                   func() time.Time
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
	var activeIdleConfigErr error
	if cfg.ActiveToolIdleTimeout <= 0 {
		cfg.ActiveToolIdleTimeout = 30 * time.Minute
		if value := os.Getenv("PACTIFY_SUB_ACTIVE_TOOL_IDLE_TIMEOUT"); value != "" {
			cfg.ActiveToolIdleTimeout, activeIdleConfigErr = time.ParseDuration(value)
			if activeIdleConfigErr != nil || cfg.ActiveToolIdleTimeout <= 0 {
				activeIdleConfigErr = fmt.Errorf("PACTIFY_SUB_ACTIVE_TOOL_IDLE_TIMEOUT must be a positive duration")
			}
		}
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
	var closingSnapshot func()
	var openingSnapshot func(bool)
	resolveQuotaReset := func() {
		if result.FailureClass != "quota" || result.QuotaResetAt != nil {
			return
		}
		model := result.ModelActual
		if model == "" {
			model = req.Model
		}
		if model == "" {
			model = cockpit.AgyDefaultModel
		}
		for i := len(result.QuotaSnapshots) - 1; i >= 0; i-- {
			if reset := result.QuotaSnapshots[i].ResetForModel(model); reset != nil {
				result.QuotaResetAt = reset
				break
			}
		}
	}
	if h.Name == "agy" && result.ConfigMode == "user" {
		result.Warnings = append(result.Warnings, "managed hooks are not installed with --with-user-config")
	}
	informational := func(w string) bool {
		return strings.HasPrefix(w, "Quota snapshot unavailable:") || strings.HasPrefix(w, "Failed to record quota snapshot:") || strings.HasPrefix(w, "Completion followup limit reached") || w == "managed hooks are not installed with --with-user-config" || w == "Opencode truncated tool output" || strings.HasPrefix(w, "Credential refresh returned to source:") || strings.HasPrefix(w, "Private configuration rewrite discarded:")
	}

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
				if !informational(warning) && !slices.Contains(cacheWarnings, warning) && !slices.Contains(st.RouteWarnings, warning) && warning != h.Experimental && warning != unverifiedWarning && warning != versionWarning && warning != dirtyWorktreeWarning {
					reason = warning
					break
				}
			}
		}
		if openingSnapshot != nil {
			openingSnapshot(false)
		}
		resolveQuotaReset()
		if e := store.Finish(id, &result, reason, finished); e != nil {
			runErr = fmt.Errorf("save terminal output: %w", e)
		}
		if closingSnapshot != nil {
			closingSnapshot()
			// Publish only the new quota metadata; the terminal status/cause is immutable.
			if e := store.WithState(context.Background(), id, func(current *State) error {
				data, e := os.ReadFile(store.ResultPath(id))
				if e != nil {
					return e
				}
				var persisted Result
				if e = json.Unmarshal(data, &persisted); e != nil {
					return e
				}
				persisted.QuotaSnapshots, persisted.RateLimit, persisted.QuotaResetAt = result.QuotaSnapshots, result.RateLimit, result.QuotaResetAt
				for _, warning := range result.Warnings {
					if informational(warning) && !slices.Contains(persisted.Warnings, warning) {
						persisted.Warnings = append(persisted.Warnings, warning)
					}
				}
				return atomicJSON(store.ResultPath(id), persisted)
			}); e != nil {
				result.Warnings = append(result.Warnings, "Failed to record quota snapshot: "+e.Error())
			}
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
	if activeIdleConfigErr != nil {
		return fail(activeIdleConfigErr)
	}
	if retryLimit < 0 || retryLimit > MaxRetries {
		return fail(fmt.Errorf("retries must be between 0 and %d", MaxRetries))
	}
	if req.Access != "write" && req.Access != "read-only" {
		return fail(fmt.Errorf("invalid access %q", req.Access))
	}
	if req.Harness == "agy" && (req.Access == "read-only" || cfg.ReadOnlyPolicy) {
		if err := cockpit.AgyReadOnlyWorkspace(req.Cwd); err != nil {
			return fail(err)
		}
	}
	if req.Access == "read-only" && h.NoReadOnly != "" {
		return fail(errors.New(h.NoReadOnly))
	}
	if requiresReadOnlyBaseline(req) {
		baseline, captureErr := captureReadOnly(req.Cwd)
		if captureErr != nil {
			return fail(fmt.Errorf("Read-only verification unavailable: %w", captureErr))
		}
		if err := store.WithState(ctx, id, func(current *State) error { current.ReadOnlyBaseline = baseline; return store.Save(*current) }); err != nil {
			return fail(err)
		}
	}

	var snap Snapshot
	if st.Worktree == nil {
		if st.Baseline != nil {
			snap = *st.Baseline
		} else {
			snap, err = TakeSnapshot(req.Cwd)
		}
		if err != nil {
			if !snap.EmptyTree || requiresReadOnlyBaseline(req) {
				if requiresReadOnlyBaseline(req) {
					err = fmt.Errorf("Read-only verification unavailable: cannot capture baseline: %w", err)
				}
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
	opts := cockpit.StartOpts{ReadOnlyPolicy: cfg.ReadOnlyPolicy, CompletionFollowups: req.CompletionFollowups, RepoDir: req.Cwd, Model: req.Model, Effort: req.Effort, Access: cockpit.Access(req.Access), Env: orchestrate.CrossVendorStrip(h.Kind), Stderr: log, Hidden: true}
	opts.MinimalConfig = result.ConfigMode == "minimal"
	marker := id
	if cfg.ProcessMarker != "" {
		marker = cfg.ProcessMarker
		if err := store.WithState(ctx, id, func(current *State) error { current.ProcessMarker = marker; return store.Save(*current) }); err != nil {
			return fail(err)
		}
	}
	opts.Env = append(opts.Env, "PACTIFY_HOME="+filepath.Dir(store.Root), "PACTIFY_JOB="+marker)
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
		harnessHome, homes, env, layouts, err = prepareHarnessHomes(ctx, h.Home, harnessHome, st.HarnessHomes, minimalHome, filepath.Join(filepath.Dir(store.Root), "credential-quarantine"))

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
	if source, ok := backend.(interface {
		Quota(context.Context, cockpit.StartOpts) (cockpit.QuotaObservation, error)
	}); ok {
		type observation struct {
			q   cockpit.QuotaObservation
			err error
		}
		collect := func(snapshotCtx context.Context) observation {
			snapshotCtx, cancel := context.WithTimeout(snapshotCtx, 20*time.Second)
			defer cancel()
			q, e := source.Quota(snapshotCtx, opts)
			return observation{q, e}
		}
		apply := func(o observation) {
			q, e := o.q, o.err
			if e != nil {
				result.Warnings = append(result.Warnings, "Quota snapshot unavailable: "+e.Error())
				return
			}
			result.QuotaSnapshots = append(result.QuotaSnapshots, q)
			result.RateLimit = q.Summary()
			if e = store.rememberQuotaSnapshot(id, h.Name, q); e != nil {
				result.Warnings = append(result.Warnings, "Failed to record quota snapshot: "+e.Error())
			}
			if result.FailureClass == "quota" && result.QuotaResetAt == nil {
				result.QuotaResetAt = q.ResetForModel(result.ModelActual)
			}
		}
		opening := make(chan observation, 1)
		go func() { opening <- collect(ctx) }()
		openingSnapshot = func(wait bool) {
			if opening == nil {
				return
			}
			if wait {
				apply(<-opening)
				opening = nil
				return
			}
			select {
			case o := <-opening:
				apply(o)
				opening = nil
			default:
			}
		}
		closingSnapshot = func() {
			// Publish the job first; allow the observed ~7s built-in command a
			// separate bounded window without delaying status or cancellation.
			closingCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
			defer cancel()
			openingSnapshot(true)
			apply(collect(closingCtx))
			resolveQuotaReset()
			if result.FailureClass == "quota" && result.QuotaResetAt != nil {
				model := result.ModelActual
				if model == "" {
					model = req.Model
				}
				if e := store.rememberQuota(QuotaRoute{Harness: req.Harness, Model: model, JobID: id, RecordedAt: cfg.Now().UTC(), ResetAt: result.QuotaResetAt, Message: result.failureMessage}); e != nil {
					result.Warnings = append(result.Warnings, "Failed to record quota snapshot: "+e.Error())
				}
			}
		}
	}
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
	var wireOffsets map[string]int64
	var turnVerification *Verification
	if h.LocalUsageBaseline != nil && h.Verify != nil && req.Resume != "" {
		var baselineErr error
		if h.LocalUsageBaselines != nil {
			wireOffsets, baselineErr = h.LocalUsageBaselines(harnessHome, sess.ThreadID())
		} else {
			wireOffset, baselineErr = h.LocalUsageBaseline(harnessHome, sess.ThreadID())
		}
		if baselineErr != nil {
			wireOffsets = nil
			wireOffset = -1 // Never count historical usage when the baseline is unknown.
			result.Warnings = append(result.Warnings, "Kimi wire baseline unavailable; tokens may be incomplete: "+baselineErr.Error())
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
	activeTools := 0
	recordEvent := func(event cockpit.Event) {
		if e := encoder.Encode(event); e != nil {
			interrupt("Failed to write log: " + e.Error())
		}
		lastActivity = cfg.Now()
		collector.Add(event)
		if event.Kind == cockpit.EventTool && event.Tool != nil {
			switch event.Tool.Phase {
			case "start":
				activeTools++
			case "end":
				activeTools = max(0, activeTools-1)
			}
		}
		if event.Kind == cockpit.EventState && (event.State == cockpit.StateTurnCompleted || event.State == cockpit.StateTurnFailed) {
			activeTools = 0
		}
		if event.Kind == cockpit.EventSystem && event.State == "warning" {
			result.Warnings = append(result.Warnings, event.Text)
		}
		if req.Access == "read-only" && cfg.ToolCallLimit > 0 && collector.ToolCalls > cfg.ToolCallLimit {
			interrupt(fmt.Sprintf("Tool-call limit exceeded (%d calls)", collector.ToolCalls))
		}
		if event.Kind == cockpit.EventState && event.State == cockpit.StateTurnFailed {
			turnFailed = true
			message := event.Err
			if message == "" {
				message = collector.ErrorMessage
			}
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
					if !collector.TurnDone && f.Class != "transient" && !(h.Name == "kimi" && h.Verify != nil && h.LocalUsageBaseline != nil) {
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
				idleBudget := cfg.IdleTimeout
				if activeTools > 0 {
					idleBudget = cfg.ActiveToolIdleTimeout
				}
				if cfg.Now().Sub(lastActivity) >= idleBudget {
					interrupt("Idle timeout")
				}
			}
		}
		if h.Name == "kimi" && h.LocalUsageBaseline != nil && h.Verify != nil && !interrupted && !canceled {
			verifyCtx, cancel := context.WithTimeout(ctx, cfg.KimiWireTimeout)
			v := h.Verify(verifyCtx, VerificationInput{SessionID: sess.ThreadID(), Info: cockpit.SessionInfo{Home: harnessHome}, WireOffset: wireOffset, WireOffsets: wireOffsets, Prompts: int(prompts.Load()), Env: opts.Env, Cwd: req.Cwd})
			cancel()
			turnVerification = &v
			if v.WireError != nil {
				turnFailed = true
				wireMessage := v.WireError.Code + ": " + v.WireError.Message
				wireRaw, _ := json.Marshal(map[string]any{"reason": v.WireReason, "error": v.WireError})
				recordEvent(cockpit.Event{Kind: cockpit.EventSystem, State: "kimi_wire_failure", Err: wireMessage, Raw: wireRaw})
				result.Warnings = append(result.Warnings, wireMessage)
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
		turnVerification = nil // A canceled retry must recover its own final wire usage.
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
		} else if err != nil && h.Name == "opencode" {
			result.Warnings = append(result.Warnings, "Session cleanup failed: "+err.Error())
			result.fallbackBlocked = true
			interrupted = true
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
	if summary := SummarizeRateLimit(collector.RateLimit); summary != "" {
		result.RateLimit = summary
	}
	// An abnormal end may omit the last accounting update, even if earlier
	// requests reported usage. Treat retained counters as a lower bound.
	recoverUsage := turnFailed || interrupted || !collector.TurnDone
	if recoverUsage || collector.UsagePartial {
		result.markUsagePartial()
	}
	if h.Verify != nil {
		verifyCtx := ctx
		if h.LocalUsageBaseline != nil {
			var cancel context.CancelFunc
			verifyCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), cfg.KimiWireTimeout)
			defer cancel()
		}

		var verified Verification
		if turnVerification != nil {
			verified = *turnVerification
		} else {
			verified = h.Verify(verifyCtx, VerificationInput{SessionID: sess.ThreadID(), Info: info, WireOffset: wireOffset, WireOffsets: wireOffsets, Prompts: int(prompts.Load()), Env: opts.Env, Cwd: req.Cwd})
		}
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
		if requiresReadOnlyBaseline(req) {
			warning = "Read-only verification unavailable: " + warning
		}
		if snap.EmptyTree {
			warning = "Changes unavailable or incomplete: " + warning
		}
		result.Warnings = append(result.Warnings, warning)
	}
	if canceled || ctx.Err() != nil {
		result.Status = StatusCanceled
	} else if (err == nil || snap.EmptyTree && !(requiresReadOnlyBaseline(req))) && collector.TurnDone && !turnFailed && !interrupted {
		result.Status = StatusSucceeded
	}
	if result.Status == StatusFailed && (!interrupted || promptFailed) {
		result.classifyTurnFailure(req.Harness, collector, cfg.Now(), promptFailed)
	}
	return result, nil
}
