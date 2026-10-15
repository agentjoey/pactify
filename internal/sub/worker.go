package sub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/orchestrate"
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
	st, err := store.Load(id)
	if err != nil {
		return result, err
	}
	if st.Status != StatusQueued {
		return result, fmt.Errorf("job %s is %s, expected queued", id, st.Status)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 10 * time.Minute
	}
	started := cfg.Now()
	st.Status = StatusRunning
	st.PID = os.Getpid()
	st.UpdatedAt = started.UTC()
	if err = store.Save(st); err != nil {
		return result, err
	}
	req := st.Request
	result = Result{JobID: id, Harness: req.Harness, ModelRequested: req.Model, Access: req.Access, Status: StatusFailed, LogPath: store.LogPath(id)}
	// Persist the result before the terminal state, so waiters never observe a partial result.
	defer func() {
		finished := cfg.Now()
		elapsed := finished.Sub(started)
		if elapsed < 0 {
			elapsed = 0
		}
		secs := int(elapsed.Seconds())
		result.Duration = fmt.Sprintf("%dm%02ds", secs/60, secs%60)
		data, e := json.MarshalIndent(result, "", "  ")
		if e == nil {
			e = os.WriteFile(store.ResultPath(id), append(data, '\n'), 0600)
		}
		if e != nil {
			runErr = fmt.Errorf("write result: %w", e)
			result.Status = StatusFailed
		}
		st.Status = result.Status
		st.UpdatedAt = finished.UTC()
		st.FinishedAt = finished.UTC()
		if runErr != nil {
			st.Error = runErr.Error()
		} else if len(result.Warnings) > 0 {
			st.Error = result.Warnings[0]
		}
		if e = store.Save(st); e != nil {
			runErr = fmt.Errorf("save terminal state: %w", e)
		}
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
	snap, err := TakeSnapshot(req.Cwd)
	if err != nil {
		return fail(err)
	}
	if h.Version != nil {
		result.CLIVersion, _ = h.Version(ctx)
	}
	if result.CLIVersion != "" {
		if previous := store.lastCLIVersionFor(req.Harness); previous != "" && previous != result.CLIVersion {
			result.Warnings = append(result.Warnings, fmt.Sprintf("CLI 版本变化：%s → %s", previous, result.CLIVersion))
		}
	}
	file, err := os.OpenFile(store.LogPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fail(err)
	}
	defer file.Close()
	log := &jobLog{w: file}
	encoder := json.NewEncoder(log)
	opts := cockpit.StartOpts{RepoDir: req.Cwd, Model: req.Model, Effort: req.Effort, Access: cockpit.Access(req.Access), Env: orchestrate.CrossVendorStrip(h.Kind), Stderr: log, Hidden: true}
	// Startup follows cancellation. Once started, Interrupt gets a grace period before Close.
	sessionCtx, stopSession := context.WithCancel(context.WithoutCancel(ctx))
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
		return fail(err)
	}
	defer sess.Close()
	result.SessionID = sess.ThreadID()
	workCtx, stopWork := context.WithCancel(sessionCtx)
	defer stopWork()
	approvalErr := make(chan error, 1)
	go func() {
		approvals := sess.Approvals()
		for {
			select {
			case <-workCtx.Done():
				return
			case a, ok := <-approvals:
				if !ok {
					return
				}
				decision := cockpit.DecisionAllow
				if req.Access == "read-only" {
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
	go func() { promptErr <- sess.Prompt(workCtx, cockpit.UserMessage{Text: req.Brief}) }()
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
			interrupt("任务已取消")
		case <-deadline:
			break loop
		case e := <-approvalErr:
			interrupt("审批失败：" + e.Error())
		case e := <-promptErr:
			promptErr = nil
			if e != nil {
				interrupt(e.Error())
			}
		case event, ok := <-events:
			if !ok {
				result.Warnings = append(result.Warnings, "会话在回合结束前关闭")
				break loop
			}
			if e := encoder.Encode(event); e != nil {
				interrupt("写日志失败：" + e.Error())
			}
			lastActivity = cfg.Now()
			collector.Add(event)
			if req.Access == "read-only" && cfg.ToolCallLimit > 0 && collector.ToolCalls > cfg.ToolCallLimit {
				interrupt(fmt.Sprintf("工具调用超限（%d 次）", collector.ToolCalls))
			}
			if event.Kind == cockpit.EventState && event.State == cockpit.StateTurnFailed {
				turnFailed = true
				message := event.Err
				if message == "" {
					message = "回合失败"
				}
				result.Warnings = append(result.Warnings, message)
			}
		case <-ticker.C:
			current := gitx.TreeFingerprint(req.Cwd)
			if current != fingerprint {
				fingerprint = current
				lastActivity = cfg.Now()
			}
			if cfg.Now().Sub(lastActivity) >= cfg.IdleTimeout {
				interrupt("空闲超时")
			}
		}
	}
	if !collector.TurnDone {
		stopWork()
	}
	// Close now (the deferred Close is then a no-op) so a hidden session's
	// archive failure can still reach the summary.
	if err := sess.Close(); errors.Is(err, cockpit.ErrArchive) {
		result.Warnings = append(result.Warnings, "会话归档失败，会留在 harness 自己的会话列表里："+err.Error())
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
	if req.Model != "" && result.ModelActual != "" && req.Model != result.ModelActual {
		result.Warnings = append(result.Warnings, "实际模型与请求不一致")
	}
	result.FinalText, result.Truncated = TruncateFinal(collector.FinalText())
	result.Diff, err = DiffSince(req.Cwd, snap)
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
