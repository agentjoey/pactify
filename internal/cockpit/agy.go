package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
)

const AgyDefaultModel = "gemini-3.8-flash-low"

// agy keeps one stream-json process alive across job turns.
type agyBackend struct {
	command func(context.Context, ...string) *exec.Cmd
	grace   time.Duration
}

func NewAgyBackend() Backend {
	return &agyBackend{command: func(ctx context.Context, args ...string) *exec.Cmd { return exec.CommandContext(ctx, "agy", args...) }, grace: 3 * time.Second}
}

// AgyModels reads the CLI's dynamic model table using the job's environment.
func AgyModels(ctx context.Context, opts StartOpts) ([]string, error) {
	return NewAgyBackend().(*agyBackend).models(ctx, opts)
}
func (b *agyBackend) models(ctx context.Context, o StartOpts) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := b.command(ctx, "models", "--model", AgyDefaultModel)
	cmd.Env = append(os.Environ(), o.Env...)
	cmd.Dir = o.RepoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("agy models --model %s: %w", AgyDefaultModel, err)
	}
	var models []string
	for _, line := range strings.Split(string(out), "\n") {
		id, _, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok && id != "" {
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("agy models --model %s returned no model ids", AgyDefaultModel)
	}
	return models, nil
}
func (b *agyBackend) Start(ctx context.Context, o StartOpts) (Session, error) {
	return b.ResumeWith(ctx, "", o)
}
func (b *agyBackend) Resume(ctx context.Context, id string) (Session, error) {
	return nil, fmt.Errorf("agy resume requires explicit options and a managed HOME; use ResumeWith")
}
func (b *agyBackend) ResumeWith(ctx context.Context, id string, o StartOpts) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o.Effort != "" && o.Effort != "low" && o.Effort != "medium" && o.Effort != "high" && o.Effort != "xhigh" && o.Effort != "max" {
		return nil, fmt.Errorf("unsupported agy effort %q; available: low, medium, high, xhigh, max", o.Effort)
	}
	if o.CompletionFollowups != nil && (*o.CompletionFollowups < 0 || *o.CompletionFollowups > 5) {
		return nil, fmt.Errorf("completion followups must be between 0 and 5")
	}
	if o.Access == AccessReadOnly || o.ReadOnlyPolicy {
		if !o.MinimalConfig {
			return nil, fmt.Errorf("agy read-only requires managed minimal config")
		}
		if err := AgyReadOnlyWorkspace(o.RepoDir); err != nil {
			return nil, err
		}
	}
	explicitModel := o.Model != ""
	if o.Model == "" {
		o.Model = AgyDefaultModel
	}
	if explicitModel {
		models, err := b.models(ctx, o)
		if err != nil {
			// The listing is only a fast pre-check: 'agy models' fetches over the
			// network and was killed by its timeout under load (AJ-650). agy itself
			// rejects an unknown model with an explicit error, and init.model is
			// verified on every turn, so a failed listing must not fail the job.
			models = []string{o.Model}
		}
		found := false
		for _, m := range models {
			if m == o.Model {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown agy model %q; available: %s", o.Model, modelChoices("agy", o.Model, models))
		}
	}
	env, err := AgyEnv(o)
	if err != nil {
		return nil, err
	}
	o.Env = env
	ctx, cancel := context.WithCancel(ctx)
	return &agySession{backend: b, opts: o, ctx: ctx, cancel: cancel, id: id, events: make(chan Event, 256), approvals: make(chan ApprovalRequest)}, nil
}

type agySession struct {
	backend   *agyBackend
	opts      StartOpts
	ctx       context.Context
	cancel    context.CancelFunc
	events    chan Event
	approvals chan ApprovalRequest
	mu        sync.Mutex
	id        string
	info      SessionInfo
	process   *agyProcess
	busy      bool
	fatal     string
	closed    bool
	totals    Usage
	closeOnce sync.Once
	closeErr  error
	runs      sync.WaitGroup
}

func (s *agySession) Events() <-chan Event              { return s.events }
func (s *agySession) Approvals() <-chan ApprovalRequest { return s.approvals }
func (s *agySession) ThreadID() string                  { s.mu.Lock(); defer s.mu.Unlock(); return s.id }
func (s *agySession) Info() SessionInfo                 { s.mu.Lock(); defer s.mu.Unlock(); return s.info }
func (s *agySession) emit(e Event) {
	select {
	case s.events <- e:
	case <-s.ctx.Done():
	}
}
func (s *agySession) Prompt(ctx context.Context, msg UserMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil {
		return fmt.Errorf("agy session closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.fatal != "" {
		return fmt.Errorf("%s", s.fatal)
	}
	if s.busy {
		return fmt.Errorf("agy turn already running")
	}
	if s.process != nil {
		select {
		case <-s.process.done:
			if s.process.cancelling.Load() {
				<-s.process.interruptDone
			}
			s.process = nil
		default:
		}
	}
	if s.process == nil {
		args := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--print-timeout", "0s"}
		args = append(args, "--model", s.opts.Model)
		if s.opts.Effort != "" {
			args = append(args, "--effort", s.opts.Effort)
		}
		if s.id != "" {
			args = append(args, "--conversation", s.id)
		}
		if s.opts.Access == AccessReadOnly {
			args = append(args, "--mode", "plan")
		}
		if s.opts.Access == AccessWrite {
			args = append(args, "--dangerously-skip-permissions")
		}
		cmd := s.backend.command(context.WithoutCancel(s.ctx), args...)
		cmd.Dir = s.opts.RepoDir
		cmd.Env = append(os.Environ(), s.opts.Env...)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		// Give forced cancellation its own process group, and bound stderr drain if
		// a descendant retains a pipe after the CLI exits.
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.Setpgid = true
		cmd.WaitDelay = time.Second
		stderr := &agyDiagnostics{sink: s.opts.Stderr}
		cmd.Stderr = stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err = procx.Start(s.ctx, cmd); err != nil {
			stdout.Close()
			return err
		}
		done := make(chan struct{})
		turnCtx, stopTurn := context.WithCancel(context.Background())
		identity, identityErr := procx.Inspect(cmd.Process.Pid)
		process := &agyProcess{cmd: cmd, stdout: stdout, done: done, ctx: turnCtx, stop: stopTurn, identity: identity, identityErr: identityErr, interruptDone: make(chan struct{}), stdin: stdin, lines: make(chan json.RawMessage, 256), diagnostics: stderr}
		s.process = process
		s.runs.Add(1)
		go func() { defer s.runs.Done(); process.read() }()
	}
	text := msg.Text
	if s.opts.SystemPrompt != "" {
		text = s.opts.SystemPrompt + "\n\n" + text
	}

	s.busy = true
	process := s.process
	s.runs.Add(1)
	go s.run(ctx, process, text)
	return nil
}

// Diagnostics are bounded and kept in memory even when no log sink is supplied.
type agyDiagnostics struct {
	mu     sync.Mutex
	text   string
	denied bool
	sink   io.Writer
}

func (d *agyDiagnostics) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sink != nil {
		_, _ = d.sink.Write(p)
	}
	d.text += string(p)
	if strings.Contains(d.text, "auto-denied") && strings.Contains(d.text, "headless") {
		d.denied = true
	}
	if len(d.text) > 65536 {
		d.text = d.text[len(d.text)-65536:]
	}
	return len(p), nil
}

type agyUsage struct {
	Input  int `json:"input_tokens"`
	Output int `json:"output_tokens"`
	Cache  int `json:"cache_read_tokens"`
}

func (u agyUsage) cockpit() Usage {
	return Usage{InputTokens: u.Input + u.Cache, OutputTokens: u.Output, TotalTokens: u.Input + u.Cache + u.Output}
}
func addAgyUsage(a, b Usage) Usage {
	return Usage{InputTokens: a.InputTokens + b.InputTokens, OutputTokens: a.OutputTokens + b.OutputTokens, TotalTokens: a.TotalTokens + b.TotalTokens}
}

type agyWire struct {
	Event string `json:"event"`
	ID    string `json:"conversation_id"`
	Init  struct {
		Model string `json:"model"`
	} `json:"init"`
	Step struct {
		Index int    `json:"step_index"`
		State string `json:"state"`
		Type  string `json:"step_type"`
		Text  string `json:"text_delta"`
		Name  string `json:"tool_name"`
		Info  struct {
			Parameters json.RawMessage `json:"parameters"`
			Output     json.RawMessage `json:"output"`
		} `json:"tool_info"`
		Usage *agyUsage `json:"usage"`
	} `json:"step_update"`
	Result struct {
		ID       string            `json:"conversation_id"`
		Status   string            `json:"status"`
		Response string            `json:"response"`
		Error    string            `json:"error"`
		Usage    *agyUsage         `json:"usage"`
		Denied   []json.RawMessage `json:"denied_actions"`
	} `json:"result"`
}

func (s *agySession) run(ctx context.Context, process *agyProcess, prompt string) {
	defer s.runs.Done()
	watched := make(chan struct{})
	stopWatch := sync.OnceFunc(func() { close(watched) })
	defer stopWatch()
	go func() {
		select {
		case <-ctx.Done():
			_ = process.interrupt(context.Background(), s.backend.grace)
		case <-s.ctx.Done():
			_ = process.interrupt(context.Background(), s.backend.grace)
		case <-watched:
		}
	}()
	emit := func(e Event) {
		select {
		case s.events <- e:
		case <-process.ctx.Done():
		case <-s.ctx.Done():
		}
	}
	followups, limit := 0, 2
	if s.opts.CompletionFollowups != nil {
		limit = *s.opts.CompletionFollowups
	}
nextTurn:
	activeTools := map[int]bool{}
	started, ended := map[int]bool{}, map[int]bool{}
	steps := map[int]Usage{}
	stepText, lastSpoke := map[int]string{}, -1
	var turn Usage
	failure, final := "", ""
	gotResult, gotModel := false, s.Info().Model != ""
	process.diagnostics.reset()
	if err := process.send(prompt); err != nil {
		failure = "agy stream interrupted: " + err.Error()
	}
readLoop:
	for failure == "" {
		var raw json.RawMessage
		select {
		case line, ok := <-process.lines:
			if !ok {
				break readLoop
			}
			raw = line
		case <-process.ctx.Done():
			failure = "interrupted"
			break readLoop
		case <-s.ctx.Done():
			failure = "interrupted"
			break readLoop
		}
		var wire agyWire
		if err := json.Unmarshal(raw, &wire); err != nil {
			failure = "invalid agy stream-json event: " + err.Error()
			_ = process.interrupt(context.Background(), s.backend.grace)
			break
		}
		switch wire.Event {
		case "init":
			gotModel = wire.Init.Model != ""
			s.mu.Lock()
			if wire.ID != "" {
				s.id = wire.ID
			}
			s.info = SessionInfo{Model: wire.Init.Model, Access: string(s.opts.Access), Effort: s.opts.Effort}
			if wire.Init.Model != s.opts.Model {
				s.fatal = fmt.Sprintf("agy model_mismatch: actual model %q does not match requested %q", wire.Init.Model, s.opts.Model)
				failure = s.fatal
			}
			s.mu.Unlock()
			emit(Event{Kind: EventSystem, State: "init", Raw: raw})
			if failure != "" {
				stopWatch()
				_ = process.interrupt(context.Background(), s.backend.grace)
				break readLoop
			}
		case "step_update":
			step := wire.Step
			if step.Type == "agent_response" {
				if step.Text != "" {
					emit(Event{Kind: EventMessage, Text: step.Text, Raw: raw})
					stepText[step.Index] += step.Text
					if strings.TrimSpace(stepText[step.Index]) != "" && step.Index >= lastSpoke {
						lastSpoke = step.Index
					}
				}
				if step.Usage != nil {
					steps[step.Index] = step.Usage.cockpit()
					turn = Usage{}
					for _, u := range steps {
						turn = addAgyUsage(turn, u)
					}
					u := addAgyUsage(s.totals, turn)
					emit(Event{Kind: EventUsage, Usage: &u, Raw: raw})
				}
			}
			if step.Type == "tool" {
				if step.State == "ACTIVE" {
					activeTools[step.Index] = true
				} else {
					delete(activeTools, step.Index)
				}
				if !started[step.Index] {
					started[step.Index] = true
					emit(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: step.Name, Text: string(step.Info.Parameters)}, Raw: raw})
				}
				if step.State != "ACTIVE" && !ended[step.Index] {
					ended[step.Index] = true
					emit(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "end", Name: step.Name, Text: string(step.Info.Output)}, Raw: raw})
				}
			}
		case "result":
			gotResult = true
			if wire.Result.ID != "" {
				s.mu.Lock()
				s.id = wire.Result.ID
				s.mu.Unlock()
			}
			if wire.Result.Status != "SUCCESS" {
				failure = wire.Result.Error
				if failure == "" {
					failure = "agy result status: " + wire.Result.Status
				}
			}
			final = wire.Result.Response
			if lastSpoke >= 0 && strings.TrimSpace(wire.Result.Response) != "" {
				final = stepText[lastSpoke]
			}
			if len(wire.Result.Denied) > 0 && strings.TrimSpace(wire.Result.Response) == "" {
				failure = "agy permission_denied: headless permission auto-denied; empty answer with denied_actions"
			}
			if len(steps) == 0 && wire.Result.Usage != nil && wire.Result.Usage.cockpit().TotalTokens > 0 {
				emit(Event{Kind: EventSystem, State: "usage_partial", Text: "agy omitted step usage; cumulative result counters were excluded"})
			}
			break readLoop
		}
	}
	if gotResult {
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-process.done:
		case <-timer.C:
		}
		timer.Stop()
	}
	select {
	case <-process.done:
		if process.err != nil && failure == "" && gotResult {
			failure = "agy process: " + process.err.Error()
		}
	default:
	}
	process.diagnostics.mu.Lock()
	denied, timedOut := process.diagnostics.denied, strings.Contains(strings.ToLower(process.diagnostics.text), "print timeout")
	process.diagnostics.mu.Unlock()
	if denied && strings.TrimSpace(final) == "" {
		failure = "agy permission_denied: headless permission auto-denied; task was not executed"
	}
	if !gotResult && failure == "" {
		failure = "agy stream interrupted: EOF without a result"
	}
	if !gotModel && failure == "" {
		failure = "agy did not report init.model"
	}
	if s.ThreadID() == "" && failure == "" {
		failure = "agy did not report a conversation id"
	}
	if ctx.Err() != nil || s.ctx.Err() != nil || process.ctx.Err() != nil {
		failure = "interrupted"
	}
	s.mu.Lock()
	fatal := s.fatal
	s.mu.Unlock()
	if fatal != "" {
		failure = fatal
	}
	if failure == "" && strings.TrimSpace(final) == "" {
		if timedOut {
			failure = "agy print_timeout: print timeout returned no content"
		} else {
			failure = "agy empty_answer: no final content"
		}
	}
	s.totals = addAgyUsage(s.totals, turn)
	u := s.totals
	emit(Event{Kind: EventUsage, Usage: &u})
	if failure == "" && (len(activeTools) > 0 || agyNeedsCompletion(final)) {
		if followups < limit {
			followups++
			prompt = "Wait for the running command to finish, then report its actual result"
			goto nextTurn
		}
		emit(Event{Kind: EventSystem, State: "warning", Text: "Completion followup limit reached; the last non-empty answer was retained"})
	}
	if final != "" {
		emit(Event{Kind: EventMessage, Text: final, Final: true})
	}
	if process.cancelling.Load() {
		<-process.interruptDone
		if process.interruptErr != nil && fatal == "" {
			failure = "interrupted; " + process.interruptErr.Error()
		}
	}
	stopWatch()
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
	state := StateTurnCompleted
	if failure != "" {
		state = StateTurnFailed
	}
	s.emit(Event{Kind: EventState, State: state, Err: failure})
}

func (s *agySession) Interrupt(ctx context.Context) error {
	s.mu.Lock()
	process := s.process
	s.mu.Unlock()
	if process == nil {
		return nil
	}
	return process.interrupt(ctx, s.backend.grace)
}
func (s *agySession) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		process, busy := s.process, s.busy
		s.mu.Unlock()
		if process != nil && !busy && !process.cancelling.Load() {
			// EOF closes a completed conversation without cancelling its normal
			// descendants. Allow the CLI (and race-instrumented peers) to flush.
			_ = process.stdin.Close()
			timer := time.NewTimer(max(2*time.Second, s.backend.grace))
			select {
			case <-process.done:
			case <-timer.C:
				s.closeErr = process.interrupt(context.Background(), s.backend.grace)
			}
			timer.Stop()
		} else if process != nil {
			s.closeErr = process.interrupt(context.Background(), s.backend.grace)
		}
		s.cancel()
		s.runs.Wait()
		close(s.events)
		close(s.approvals)
	})
	return s.closeErr
}

// Observed headless failures; only confirmed login hints are treated as auth.
// The managed-home login hint is emitted by sub/managed_home.go.
var agyFailureRules = []failureRule{
	{"model_mismatch", "model_mismatch"},
	{"individual quota reached", "quota"},
	{"run agy login first", "auth"},
	// Confirmed by the official CHANGELOG, version 1.3.3.
	{"authentication expired. please log back in.", "auth"},
	{"unavailable (code 503)", "transient"},
	{": eof", "transient"},
	{"stream interrupted", "transient"},
	{"permission_denied", "permission"},
	{"empty_answer", "empty_answer"},
	{"print_timeout", "timeout"},
	{"incomplete_answer", "incomplete"},
	{"the stream was interrupted. please continue", "transient"},
}

var agyResetDuration = regexp.MustCompile(`(?i)resets in ([0-9]+(?:h[0-9]+m(?:[0-9]+s)?|h(?:[0-9]+s)?|m(?:[0-9]+s)?|s))`)

func agyFailure(message string, now time.Time) Failure {
	f := matchFailure(message, agyFailureRules)
	if f.Class == "other" && agyTransientFailure.MatchString(message) {
		f.Class = "transient"
	}
	if f.Class == "quota" {
		if m := agyResetDuration.FindStringSubmatch(message); len(m) > 1 {
			if d, err := time.ParseDuration(m[1]); err == nil {
				reset := now.Add(d).UTC()
				f.ResetAt = &reset
			}
		}
	}
	return f
}

// Match explicit future promises, not completed commands or quoted instructions.
var agyStartedWork = regexp.MustCompile(`(?i)\b(?:started|launched|running|pending|background)\b|已启动|正在运行`)
var agyMissingResult = regexp.MustCompile(`(?i)\b(?:no (?:results?|output) yet|still running|not (?:finished|completed)|(?:check|inspect|review) (?:the )?(?:results?|output) later|(?:once|when) (?:it|they|the tests?|the command) (?:finishes?|completes?))\b|完成后再查看|稍后再看`)
var agyActualResult = regexp.MustCompile(`(?i)\b(?:all \d*\s*tests? pass|tests? (?:passed|pass|failed)|done[.!]|results? below|no longer an issue|follow-up pr|after you push)\b|所有测试通过|如需要`)

func agyNeedsCompletion(text string) bool {
	text = strings.TrimSpace(text)
	if agyActualResult.MatchString(text) {
		return false
	}
	paragraphs := strings.Split(text, "\n\n")
	tail := paragraphs[len(paragraphs)-1]
	return agyStartedWork.MatchString(tail) && agyMissingResult.MatchString(tail)
}

var agyTransientFailure = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:EOF|503)(?:$|[^a-z0-9])`)

func (d *agyDiagnostics) reset() { d.mu.Lock(); d.text = ""; d.denied = false; d.mu.Unlock() }
