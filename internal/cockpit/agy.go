package cockpit

import (
	"bufio"
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

// AgyNoReadOnly explains the disabled access mode at both routing and backend boundaries.
const AgyNoReadOnly = "Read-only access is not offered for agy in the first release: all 3 evaluation read-only tasks ended without a conclusion after headless mode auto-denied shell commands. Use codex --read-only for read-only tasks; use agy --write only with explicit write permission"

// agy runs one isolated CLI process per turn. No child stream inherits host stdio.
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
	cmd := b.command(ctx, "models")
	cmd.Env = append(os.Environ(), o.Env...)
	cmd.Dir = o.RepoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("agy models: %w", err)
	}
	var models []string
	for _, line := range strings.Split(string(out), "\n") {
		id, _, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok && id != "" {
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("agy models returned no model ids")
	}
	return models, nil
}
func (b *agyBackend) Start(ctx context.Context, o StartOpts) (Session, error) {
	return b.ResumeWith(ctx, "", o)
}
func (b *agyBackend) Resume(ctx context.Context, id string) (Session, error) {
	return b.ResumeWith(ctx, id, StartOpts{})
}
func (b *agyBackend) ResumeWith(ctx context.Context, id string, o StartOpts) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o.Access == AccessReadOnly {
		return nil, fmt.Errorf("%s", AgyNoReadOnly)
	}
	if o.Model != "" {
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
	if s.process != nil {
		select {
		case <-s.process.done:
			if s.process.cancelling.Load() {
				<-s.process.interruptDone
			}
		default:
			return fmt.Errorf("agy turn already running")
		}
	}
	args := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--print-timeout", "0s"}
	if s.opts.Model != "" {
		args = append(args, "--model", s.opts.Model)
	}
	if s.opts.Effort != "" {
		args = append(args, "--effort", s.opts.Effort)
	}
	if s.id != "" {
		args = append(args, "--conversation", s.id)
	}
	if s.opts.Access == AccessWrite {
		args = append(args, "--dangerously-skip-permissions")
	}
	// Official stdin wire format: https://www.agy.dev/docs/cli/headless/
	text := msg.Text
	if s.opts.SystemPrompt != "" {
		text = s.opts.SystemPrompt + "\n\n" + text
	}
	input, _ := json.Marshal(map[string]any{"event": "user", "message": map[string]string{"content": text}})
	cmd := s.backend.command(context.WithoutCancel(s.ctx), args...)
	cmd.Dir = s.opts.RepoDir
	cmd.Env = append(os.Environ(), s.opts.Env...)
	cmd.Stdin = strings.NewReader(string(input) + "\n")
	// Give forced cancellation its own process group, and bound stderr drain if
	// a descendant retains a pipe after the CLI exits.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.WaitDelay = time.Second
	stderr := &agyDiagnostics{}
	cmd.Stderr = stderr
	if s.opts.Stderr != nil {
		cmd.Stderr = io.MultiWriter(stderr, s.opts.Stderr)
	}
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
	process := &agyProcess{cmd: cmd, stdout: stdout, done: done, stop: stopTurn, identity: identity, identityErr: identityErr, interruptDone: make(chan struct{})}
	s.process = process
	s.info = SessionInfo{}
	s.runs.Add(1)
	go s.run(ctx, turnCtx, process, stderr)
	return nil
}

// Diagnostics are bounded and kept in memory even when no log sink is supplied.
type agyDiagnostics struct {
	text   string
	denied bool
}

func (d *agyDiagnostics) Write(p []byte) (int, error) {
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
		ID       string    `json:"conversation_id"`
		Status   string    `json:"status"`
		Response string    `json:"response"`
		Error    string    `json:"error"`
		Usage    *agyUsage `json:"usage"`
	} `json:"result"`
}

func (s *agySession) run(ctx, turnCtx context.Context, process *agyProcess, stderr *agyDiagnostics) {
	cmd, stdout, done := process.cmd, process.stdout, process.done
	stopTurn := process.stop
	defer s.runs.Done()
	defer stopTurn()
	watched := make(chan struct{})
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
		case <-turnCtx.Done():
		case <-s.ctx.Done():
		}
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 65536), 16*1024*1024)
	started, ended := map[int]bool{}, map[int]bool{}
	steps := map[int]Usage{}
	// agy's result.response concatenates every agent_response step of the turn,
	// including narration between tool calls; the final reply is the last step
	// that said something (AJ-650 eval, agy 1.3.5).
	stepText, lastSpoke := map[int]string{}, -1
	var turn Usage
	var resultUsage *Usage
	failure, final := "", ""
	gotResult := false
	gotModel := false
	usage := func(u Usage, raw json.RawMessage) { emit(Event{Kind: EventUsage, Usage: &u, Raw: raw}) }
	for scanner.Scan() {
		raw := append(json.RawMessage(nil), scanner.Bytes()...)
		var wire agyWire
		if err := json.Unmarshal(raw, &wire); err != nil {
			failure = "invalid agy stream-json event: " + err.Error()
			continue
		}
		switch wire.Event {
		case "init":
			gotModel = wire.Init.Model != ""
			s.mu.Lock()
			if wire.ID != "" {
				s.id = wire.ID
			}
			s.info.Model = wire.Init.Model
			s.info.Access = string(s.opts.Access)
			s.info.Effort = s.opts.Effort
			s.mu.Unlock()
			if s.opts.Model != "" && wire.Init.Model != s.opts.Model {
				failure = fmt.Sprintf("agy actual model %q does not match requested %q", wire.Init.Model, s.opts.Model)
			}
			emit(Event{Kind: EventSystem, State: "init", Raw: raw})
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
					usage(addAgyUsage(s.totals, turn), raw)
				}
			}
			if step.Type == "tool" {
				if !started[step.Index] {
					started[step.Index] = true
					emit(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: step.Name, Text: string(step.Info.Parameters)}, Raw: raw})
				}
				if (step.State == "DONE" || step.State == "ERROR") && !ended[step.Index] {
					ended[step.Index] = true
					emit(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "end", Name: step.Name, Text: string(step.Info.Output)}, Raw: raw})
				}
			}
		case "result":
			if gotResult {
				failure = "agy returned multiple results for one prompt"
			}
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
			if lastSpoke >= 0 {
				final = stepText[lastSpoke]
			}
			if wire.Result.Usage != nil {
				u := wire.Result.Usage.cockpit()
				resultUsage = &u
			}
		}
	}
	if err := scanner.Err(); err != nil {
		failure = "read agy stream: " + err.Error()
		_ = cmd.Process.Kill()
	}
	err := cmd.Wait()
	stdout.Close()
	close(watched)
	if stderr.denied {
		failure = "agy headless permission auto-denied; task was not executed"
	} else if err != nil && failure == "" {
		failure = "agy process: " + err.Error()
	}
	if !gotResult && failure == "" {
		failure = "agy exited without a result"
	}
	if !gotModel && failure == "" {
		failure = "agy did not report init.model"
	}
	if s.ThreadID() == "" && failure == "" {
		failure = "agy did not report a conversation id"
	}
	if ctx.Err() != nil || s.ctx.Err() != nil || turnCtx.Err() != nil {
		failure = "interrupted"
	}
	if resultUsage != nil {
		if failure == "" {
			turn = *resultUsage
		} else {
			// An error result can contain empty or incomplete usage. Retain the
			// known step totals without counting the result a second time.
			turn.InputTokens = max(turn.InputTokens, resultUsage.InputTokens)
			turn.OutputTokens = max(turn.OutputTokens, resultUsage.OutputTokens)
			turn.TotalTokens = turn.InputTokens + turn.OutputTokens
		}
	}
	s.totals = addAgyUsage(s.totals, turn)
	usage(s.totals, nil)
	if final != "" {
		emit(Event{Kind: EventMessage, Text: final, Final: true})
	}
	close(done)
	if process.cancelling.Load() {
		<-process.interruptDone
		if process.interruptErr != nil {
			failure = "interrupted; " + process.interruptErr.Error()
		}
	}
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
		s.mu.Unlock()
		s.cancel()
		s.closeErr = s.Interrupt(context.Background())
		s.runs.Wait()
		close(s.events)
		close(s.approvals)
	})
	return s.closeErr
}

// Observed headless failures; only confirmed login hints are treated as auth.
// The managed-home login hint is emitted by sub/managed_home.go.
var agyFailureRules = []failureRule{
	{"individual quota reached", "quota"},
	{"run agy login first", "auth"},
	// Confirmed by the official CHANGELOG, version 1.3.3.
	{"authentication expired. please log back in.", "auth"},
	{"unavailable (code 503)", "transient"},
	{": eof", "transient"},
	{"the stream was interrupted. please continue", "transient"},
}

var agyResetDuration = regexp.MustCompile(`(?i)resets in ([0-9]+(?:h[0-9]+m(?:[0-9]+s)?|h(?:[0-9]+s)?|m(?:[0-9]+s)?|s))`)

func agyFailure(message string, now time.Time) Failure {
	f := matchFailure(message, agyFailureRules)
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
