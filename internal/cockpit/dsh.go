package cockpit

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit/golden"
	"github.com/agentjoey/pactify/internal/vendorenv"
)

const DSHDefaultModel = "deepseek-v4-flash"

type dshBackend struct{}

func NewDSHBackend() Backend { return dshBackend{} }
func (b dshBackend) Start(ctx context.Context, o StartOpts) (Session, error) {
	return b.start(ctx, "", o)
}
func (b dshBackend) Resume(ctx context.Context, id string) (Session, error) {
	return b.ResumeWith(ctx, id, StartOpts{})
}
func (b dshBackend) ResumeWith(ctx context.Context, id string, o StartOpts) (Session, error) {
	s, err := b.start(ctx, id, o)
	if err != nil {
		return nil, err
	}
	_, err = s.(*dshSession).call(ctx, "session/resume", map[string]string{"sessionId": id})
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

type dshReply struct {
	Result json.RawMessage
	Error  *struct {
		Code    int
		Message string
	}
}

// DSHUsageRecord is independently accumulated from assistant/message records,
// rather than reconstructed from the unified usage envelope. It is process-local:
// SDK resume does not replay historical events.
type DSHUsageRecord struct {
	InputTokens, OutputTokens int
	Model                     string
	Recorded                  bool
}
type dshSession struct {
	cmd              *exec.Cmd
	in               io.WriteCloser
	stdout, stderr   io.ReadCloser
	stderrDone       chan struct{}
	secrets          []string
	failed           chan struct{}
	failOnce         sync.Once
	broken           error
	processMu        sync.Mutex
	reaping          bool
	closeErr         error
	opts             StartOpts
	id               string
	mu               sync.Mutex
	writeMu          chan struct{}
	next             int
	pending          map[int]chan dshReply
	done             chan struct{}
	reaped           chan struct{}
	events           chan Event
	approvals        chan ApprovalRequest
	stopping         chan struct{}
	closeOnce        sync.Once
	turn             sync.WaitGroup
	active, canceled bool
	closed           bool
	reason, final    string
	reasonDetails    string
	info             SessionInfo
	usage            Usage
	record           DSHUsageRecord
	toolNames        map[string]string
}

func (b dshBackend) start(ctx context.Context, id string, o StartOpts) (Session, error) {
	if o.Effort != "" && o.Effort != "off" && o.Effort != "low" && o.Effort != "high" && o.Effort != "max" {
		return nil, fmt.Errorf("dsh effort must be off, low, high or max")
	}
	model := o.Model
	provider := "deepseek-official"
	if model == "" {
		model = DSHDefaultModel
	}
	if p, m, ok := strings.Cut(model, "/"); ok {
		provider, model = p, m
	}
	mode := "workspace-write"
	if o.Access == AccessReadOnly {
		mode = "read-only"
	}
	cmd := exec.Command("dsh", "--profile", "sdk")
	cmd.Dir = o.RepoDir
	cmd.Env = append(append(os.Environ(), o.Env...), "DSH_PERMISSION_MODE="+mode, "DSH_TELEMETRY_DISABLED=1")
	secrets := dshCredentialValues(cmd.Env)
	if !strings.Contains(o.Model, "/") {
		cmd.Env = append(cmd.Env, vendorenv.Strip("dsh-official")...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if id == "" {
		var buf [16]byte
		if _, err = rand.Read(buf[:]); err != nil {
			return nil, err
		}
		id = hex.EncodeToString(buf[:])
	}
	s := &dshSession{stdout: out, stderr: stderr, stderrDone: make(chan struct{}), secrets: secrets, failed: make(chan struct{}), cmd: cmd, in: in, opts: o, id: id, pending: map[int]chan dshReply{}, toolNames: map[string]string{}, writeMu: make(chan struct{}, 1), stopping: make(chan struct{}), done: make(chan struct{}), reaped: make(chan struct{}), events: make(chan Event, 256), approvals: make(chan ApprovalRequest), info: SessionInfo{Model: model, Provider: provider, Effort: o.Effort, Access: mode}}
	for _, e := range cmd.Env {
		if v, ok := strings.CutPrefix(e, "DSH_HOME="); ok {
			s.info.Home = v
		}
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	go s.read(out)
	go func() {
		defer close(s.stderrDone)
		// Forward sanitized stderr only: SDK diagnostics may contain secrets.
		// Redact known credential values, secret fields, bearer tokens and paths.
		scan := bufio.NewScanner(stderr)
		scan.Buffer(make([]byte, 4096), 4<<20)
		for scan.Scan() {
			line := s.sanitize(scan.Text())
			sink := o.Stderr
			if sink == nil {
				sink = os.Stderr
			}
			fmt.Fprintln(sink, line)
			if o.Transcript != nil {
				o.Transcript.Observe("stderr", []byte(line))
			}
		}
		if scan.Err() != nil {
			_, _ = io.Copy(io.Discard, stderr)
		}
	}()
	params := map[string]any{"cwd": o.RepoDir, "provider": provider, "model": model}
	if o.Effort != "" {
		params["reasoningEffort"] = o.Effort
	}
	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result, err := s.call(initCtx, "initialize", params)
	if err == nil {
		var r struct {
			ServerInfo struct{ Name string }
		}
		if json.Unmarshal(result, &r) != nil || r.ServerInfo.Name != "deepseek-harness-sdk-runtime" {
			err = fmt.Errorf("dsh SDK initialize returned invalid serverInfo")
		}
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Interrupt(context.Background())
		case <-s.done:
		}
	}()
	return s, nil
}

// DSHSupportedVersion accepts only the pinned 0.2.1 release line, including npm prereleases.
var dshVersion = regexp.MustCompile(`^v?0\.2\.1(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?(?:\+[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$`)

func DSHSupportedVersion(v string) bool { return dshVersion.MatchString(v) }

func (s *dshSession) recordFrame(channel string, b []byte) {
	if s.opts.Transcript == nil {
		return
	}
	s.opts.Transcript.Observe(channel, []byte(s.sanitize(string(b))))
}

// Redact exact environment credentials before generic sanitization, which also
// covers structured secret fields, bearer tokens and local paths.
func (s *dshSession) sanitize(text string) string {
	for _, value := range s.secrets {
		text = strings.ReplaceAll(text, value, golden.Redacted)
	}
	return golden.Sanitize(text)
}

// Serialize every stdin frame, including replies to SDK requests. Both lock
// acquisition and pipe writes are bounded when the runtime stops reading.
func (s *dshSession) writeFrame(ctx context.Context, b []byte) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	select {
	case s.writeMu <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.writeMu }()
	if err := s.failure(); err != nil {
		return err
	}
	pipe, ok := s.in.(interface{ SetWriteDeadline(time.Time) error })
	if !ok {
		return fmt.Errorf("dsh SDK stdin does not support write deadlines")
	}
	deadline, _ := ctx.Deadline()
	if err := pipe.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("dsh SDK stdin deadline failed: %w", err)
	}
	s.recordFrame("stdin", b)
	frame := append(b, '\n')
	n, err := s.in.Write(frame)
	if err == nil && n != len(frame) {
		err = io.ErrShortWrite
	}
	if err != nil {
		s.breakSession(fmt.Errorf("dsh SDK stream write failed: %w", err))
	}
	return err
}

func (s *dshSession) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	if s.broken != nil {
		err := s.broken
		s.mu.Unlock()
		return nil, err
	}
	s.next++
	id := s.next
	ch := make(chan dshReply, 1)
	s.pending[id] = ch
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, id); s.mu.Unlock() }()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	err := s.writeFrame(ctx, b)
	if err != nil {
		return nil, fmt.Errorf("dsh SDK write failed")
	}
	select {
	case r := <-ch:
		if err := s.failure(); err != nil {
			return nil, err
		}
		if r.Error != nil {
			return nil, fmt.Errorf("%s", s.sanitize(fmt.Sprintf("dsh SDK %s: RPC error %d: %s", method, r.Error.Code, r.Error.Message)))
		}
		return r.Result, nil
	case <-s.failed:
		return nil, s.failure()
	case <-s.done:
		if err := s.failure(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("dsh SDK process ended")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *dshSession) read(r io.Reader) {
	defer close(s.done)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), 16<<20)
	for sc.Scan() {
		b := append([]byte(nil), sc.Bytes()...)
		if s.opts.Transcript != nil {
			s.recordFrame("stdout", b)
		}
		var msg struct {
			ID     json.RawMessage
			Method *string
			Params json.RawMessage
			Result json.RawMessage
			Error  *struct {
				Code    int
				Message string
			}
		}
		if json.Unmarshal(b, &msg) != nil {
			continue
		}
		if len(msg.ID) > 0 && msg.Method != nil {
			reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32601, "message": "Method not found"}})
			if err := s.writeFrame(context.Background(), reply); err != nil {
				// No approval channel exists. A peer that cannot receive our
				// denial is a protocol failure, not a user cancellation.
				s.breakSession(fmt.Errorf("dsh SDK response write failed"))
				return
			}
			continue
		}
		if len(msg.ID) > 0 {
			var id int
			if json.Unmarshal(msg.ID, &id) != nil {
				continue
			}
			s.mu.Lock()
			ch := s.pending[id]
			s.mu.Unlock()
			if ch != nil {
				select {
				case ch <- dshReply{Result: msg.Result, Error: msg.Error}:
				default:
				}
			}
			continue
		}
		if msg.Method != nil && *msg.Method == "session.event" {
			s.notification(msg.Params)
		}
	}
}
func (s *dshSession) emit(e Event) {
	select {
	case s.events <- e:
	case <-s.stopping:
	}
}
func (s *dshSession) notification(raw json.RawMessage) {
	var n struct {
		SessionID string
		Event     struct {
			Type string
			Data json.RawMessage
		}
	}
	if json.Unmarshal(raw, &n) != nil {
		return
	}
	d := n.Event.Data
	var a struct {
		Name, Arguments, CallID string
		Reason                  json.RawMessage
		Message                 struct {
			Content    []struct{ Type, Text string }
			Source     struct{ Provider, Model string }
			IsError    bool
			ToolCallID string
		}
		Usage *struct{ InputTokens, CacheReadTokens, CacheWriteTokens, OutputTokens, TotalTokens int }
		Error json.RawMessage
	}
	if json.Unmarshal(d, &a) != nil {
		return
	}
	var events []Event
	s.mu.Lock()
	switch n.Event.Type {
	case "assistant/message":
		if a.Usage != nil {
			input := a.Usage.InputTokens + a.Usage.CacheReadTokens + a.Usage.CacheWriteTokens
			s.usage.InputTokens += input
			s.usage.OutputTokens += a.Usage.OutputTokens
			s.usage.TotalTokens += a.Usage.TotalTokens
			s.record.InputTokens += input
			s.record.OutputTokens += a.Usage.OutputTokens
			s.record.Recorded = true
			u := s.usage
			events = append(events, Event{Kind: EventUsage, Usage: &u})
		}
		if n.SessionID == s.id {
			var texts []string
			for _, b := range a.Message.Content {
				if b.Type == "text" {
					texts = append(texts, b.Text)
				}
			}
			if len(texts) > 0 {
				s.final = strings.Join(texts, "\n")
			}
			if a.Message.Source.Model != "" {
				s.info.Model = a.Message.Source.Model
				s.info.Provider = a.Message.Source.Provider
				s.record.Model = a.Message.Source.Model
				if strings.Contains(s.opts.Model, "/") {
					s.info.Model = a.Message.Source.Provider + "/" + a.Message.Source.Model
					s.record.Model = s.info.Model
				}
			}
		}
	case "tool/call":
		s.toolNames[n.SessionID+"/"+a.CallID] = a.Name
		events = append(events, Event{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: a.Name, Text: a.Arguments}})
	case "tool/result":
		var texts []string
		for _, b := range a.Message.Content {
			if b.Type == "text" {
				texts = append(texts, b.Text)
			}
		}
		events = append(events, Event{Kind: EventTool, Tool: &ToolEvent{Phase: "end", Name: s.toolNames[n.SessionID+"/"+a.Message.ToolCallID], Text: strings.Join(texts, "\n")}})
		delete(s.toolNames, n.SessionID+"/"+a.Message.ToolCallID)
	case "turn/end":
		if n.SessionID == s.id {
			var reason struct{ Kind string }
			_ = json.Unmarshal(a.Reason, &reason)
			s.reason = reason.Kind
			s.reasonDetails = s.sanitize(strings.Join(dshReasonDetails(a.Reason, a.Error), "; "))
		}
	}
	s.mu.Unlock()
	// read dispatches notifications serially; emitting here preserves wire order
	// while state readers and cancellation remain independent of backpressure.
	for _, event := range events {
		s.emit(event)
	}

}
func (s *dshSession) Prompt(ctx context.Context, msg UserMessage) error {
	s.mu.Lock()
	if s.broken != nil {
		err := s.broken
		s.mu.Unlock()
		return err
	}
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("dsh session is closed")
	}
	if s.active {
		s.mu.Unlock()
		return fmt.Errorf("dsh turn already running")
	}
	s.active = true
	s.reason = ""
	s.reasonDetails = ""
	s.final = ""
	s.turn.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.turn.Done()
		s.emit(Event{Kind: EventState, State: "turn_started"})
		text := msg.Text
		if s.opts.SystemPrompt != "" {
			text = s.opts.SystemPrompt + "\n\n" + text
		}
		_, err := s.call(ctx, "session/prompt", map[string]any{"sessionId": s.id, "contentBlocks": []map[string]string{{"type": "text", "text": text}}})
		if err == nil {
			_, err = s.call(ctx, "session/wait", map[string]string{"sessionId": s.id})
		}
		s.mu.Lock()
		reason, details, final, canceled := s.reason, s.reasonDetails, s.final, s.canceled
		s.active = false
		s.mu.Unlock()
		if canceled {
			s.emit(Event{Kind: EventState, State: StateTurnFailed, Err: "canceled"})
			return
		}
		if err != nil {
			if ctx.Err() != nil {
				_ = s.Interrupt(context.Background())
			}
			s.emit(Event{Kind: EventState, State: StateTurnFailed, Err: err.Error()})
			return
		}
		if reason != "completed" {
			if reason == "" {
				reason = "missing turn/end"
			}
			if details != "" {
				reason += ": " + details
			}
			s.emit(Event{Kind: EventState, State: StateTurnFailed, Err: s.sanitize("dsh turn: " + reason)})
			return
		}
		s.emit(Event{Kind: EventMessage, Final: true, Text: final})
		s.emit(Event{Kind: EventState, State: StateTurnCompleted})
	}()
	return nil
}

// signalGroup is serialized with the transition to reaping. Until that
// transition the unreaped leader reserves its PID/PGID, even after exit.
func (s *dshSession) signalGroup(sig syscall.Signal) error {
	s.processMu.Lock()
	defer s.processMu.Unlock()
	if s.reaping {
		return nil
	}
	err := syscall.Kill(-s.cmd.Process.Pid, sig)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}
func (s *dshSession) failure() error { s.mu.Lock(); defer s.mu.Unlock(); return s.broken }
func (s *dshSession) breakSession(err error) {
	s.failOnce.Do(func() {
		s.mu.Lock()
		s.broken = err
		s.mu.Unlock()
		close(s.failed)
		_ = s.signalGroup(syscall.SIGTERM)
		time.AfterFunc(time.Second, func() { _ = s.signalGroup(syscall.SIGKILL) })
	})
}
func (s *dshSession) Interrupt(context.Context) error {
	s.mu.Lock()
	s.canceled = true
	s.mu.Unlock()
	s.breakSession(fmt.Errorf("dsh session canceled"))
	return nil
}
func (s *dshSession) Events() <-chan Event              { return s.events }
func (s *dshSession) Approvals() <-chan ApprovalRequest { return s.approvals }
func (s *dshSession) ThreadID() string                  { return s.id }
func (s *dshSession) Info() SessionInfo                 { s.mu.Lock(); defer s.mu.Unlock(); return s.info }
func (s *dshSession) DSHUsage() DSHUsageRecord          { s.mu.Lock(); defer s.mu.Unlock(); return s.record }
func (s *dshSession) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		close(s.stopping)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, _ = s.call(ctx, "shutdown", map[string]any{})
		cancel()
		select {
		case <-s.done:
		case <-time.After(time.Second):
		}

		// Always kill the group before reaping, including a leader that exited
		// successfully while children remain. No later signal may use this PGID.
		s.processMu.Lock()
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		s.reaping = true
		s.processMu.Unlock()

		drained := make(chan struct{})
		go func() { <-s.done; <-s.stderrDone; close(drained) }()
		select {
		case <-drained:
		case <-time.After(250 * time.Millisecond):
		}
		// Detached descendants can retain both pipes despite the group kill. Close
		// our read ends rather than waiting forever for their EOF.
		_ = s.stdout.Close()
		_ = s.stderr.Close()
		_ = s.in.Close()
		go func() { _ = s.cmd.Wait(); close(s.reaped) }()
		select {
		case <-s.reaped:
		case <-time.After(time.Second):
			s.closeErr = fmt.Errorf("dsh SDK leader did not exit after SIGKILL")
		}

		finished := make(chan struct{})
		go func() { <-s.done; s.turn.Wait(); close(s.events); close(s.approvals); close(finished) }()
		select {
		case <-finished:
		case <-time.After(time.Second):
			if s.closeErr == nil {
				s.closeErr = fmt.Errorf("dsh SDK event reader did not stop")
			}
		}
	})
	return s.closeErr
}

// Snapshot inherited and overridden values before stripping the launch env.
// Longest-first replacement also conceals credentials with shared prefixes.
func dshCredentialValues(env []string) []string {
	names := map[string]bool{"DEEPSEEK_API_KEY": true}
	for _, key := range vendorenv.APIKeys {
		names[key] = true
	}
	var values []string
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && names[key] && value != "" {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return values
}

// Terminal diagnostics have varied layouts. Retain only optional code/message
// scalars in recognized containers; malformed/missing fields do not lose kind.
func dshReasonDetails(values ...json.RawMessage) []string {
	var parts []string
	var visit func(json.RawMessage, int)
	visit = func(raw json.RawMessage, depth int) {
		if depth > 4 {
			return
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return
		}
		for _, key := range []string{"code", "message"} {
			value := fields[key]
			var text string
			if json.Unmarshal(value, &text) == nil && text != "" {
				parts = append(parts, key+"="+text)
				continue
			}
			if key == "code" {
				var n json.Number
				if json.Unmarshal(value, &n) == nil && n != "" {
					parts = append(parts, "code="+n.String())
				}
			}
		}
		for _, key := range []string{"error", "details", "cause"} {
			visit(fields[key], depth+1)
		}
	}
	for _, raw := range values {
		visit(raw, 0)
	}
	return parts
}
