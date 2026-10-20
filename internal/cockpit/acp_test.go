package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
)

func TestMapACPUpdateToEventMessage(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}}`)
	u := acp.SessionUpdate{Kind: "agent_message_chunk", Raw: raw}

	e, ok := MapACPUpdateToEvent(u)
	if !ok {
		t.Fatal("expected event")
	}
	want := Event{Kind: EventMessage, Text: "hello", Raw: raw}
	if !reflect.DeepEqual(e, want) {
		t.Fatalf("got %+v, want %+v", e, want)
	}
}

func TestMapACPUpdateToEventUsage(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"usage","usage":{"inputTokens":10,"outputTokens":5,"cost":0.0012}}`)
	u := acp.SessionUpdate{
		Kind:  "usage",
		Usage: &acp.Usage{InputTokens: 10, OutputTokens: 5, Cost: 0.0012},
		Raw:   raw,
	}

	e, ok := MapACPUpdateToEvent(u)
	if !ok {
		t.Fatal("expected event")
	}
	want := Event{
		Kind: EventUsage,
		Usage: &Usage{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
			CostUSD:      0.0012,
		},
		Raw: raw,
	}
	if !reflect.DeepEqual(e, want) {
		t.Fatalf("got %+v, want %+v", e, want)
	}
}

func TestMapACPUpdateToEventTool(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"tool_call","title":"ls","kind":"bash"}`)
	u := acp.SessionUpdate{Kind: "tool_call", Raw: raw}
	e, _ := MapACPUpdateToEvent(u)
	if e.Kind != EventTool || e.Tool == nil || e.Tool.Phase != "start" || e.Tool.Name != "ls" {
		t.Fatalf("tool_call event mismatch: %+v", e)
	}

	raw2 := json.RawMessage(`{"sessionUpdate":"tool_call_update","title":"ls"}`)
	u2 := acp.SessionUpdate{Kind: "tool_call_update", Raw: raw2}
	e2, _ := MapACPUpdateToEvent(u2)
	if e2.Kind != EventTool || e2.Tool == nil || e2.Tool.Phase != "output" || e2.Tool.Name != "ls" {
		t.Fatalf("tool_call_update event mismatch: %+v", e2)
	}
}

func TestMapDecisionToOutcomeAllow(t *testing.T) {
	opts := []acp.PermissionOption{
		{OptionID: "opt-deny", Kind: "deny"},
		{OptionID: "opt-allow", Kind: "allow"},
	}
	out := MapDecisionToOutcome(DecisionAllow, opts)
	if out.OptionID != "opt-allow" || out.Cancelled {
		t.Fatalf("got %+v, want allow option", out)
	}
}

func TestMapDecisionToOutcomeAllowForSession(t *testing.T) {
	opts := []acp.PermissionOption{
		{OptionID: "opt-allow", Kind: "allow"},
		{OptionID: "opt-always", Kind: "always-session"},
	}
	out := MapDecisionToOutcome(DecisionAllowForSession, opts)
	if out.OptionID != "opt-always" || out.Cancelled {
		t.Fatalf("got %+v, want always-session option", out)
	}
}

func TestMapDecisionToOutcomeDeny(t *testing.T) {
	opts := []acp.PermissionOption{
		{OptionID: "opt-allow", Kind: "allow"},
		{OptionID: "opt-reject", Kind: "reject"},
	}
	out := MapDecisionToOutcome(DecisionDeny, opts)
	if out.OptionID != "opt-reject" || out.Cancelled {
		t.Fatalf("got %+v, want reject option", out)
	}
}

func TestMapDecisionToOutcomeDenyFallsBackToCancelled(t *testing.T) {
	opts := []acp.PermissionOption{
		{OptionID: "opt-allow", Kind: "allow"},
	}
	out := MapDecisionToOutcome(DecisionDeny, opts)
	if !out.Cancelled {
		t.Fatalf("got %+v, want cancelled fallback", out)
	}
}

func TestPermissionBridgeNoDeadlock(t *testing.T) {
	approvals := make(chan ApprovalRequest, 1)
	done := make(chan struct{})
	defer close(done)

	handler := newPermissionHandler(approvals, done)

	req := acp.PermissionRequest{
		ToolCall: acp.PermissionToolCall{ToolCallID: "tc1", Title: "rm -rf /"},
		Options: []acp.PermissionOption{
			{OptionID: "opt-allow", Kind: "allow"},
			{OptionID: "opt-deny", Kind: "deny"},
		},
	}

	outcomeCh := make(chan acp.PermissionOutcome, 1)
	go func() {
		outcomeCh <- handler(req)
	}()

	var ar ApprovalRequest
	select {
	case ar = <-approvals:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval request")
	}

	if ar.ToolName != "rm -rf /" {
		t.Fatalf("tool name = %q, want %q", ar.ToolName, "rm -rf /")
	}
	if err := ar.Respond(DecisionAllow); err != nil {
		t.Fatalf("Respond failed: %v", err)
	}

	select {
	case out := <-outcomeCh:
		if out.OptionID != "opt-allow" || out.Cancelled {
			t.Fatalf("got %+v, want allow option", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for permission outcome")
	}
}

func TestPermissionBridgeDoneCancels(t *testing.T) {
	approvals := make(chan ApprovalRequest, 1)
	done := make(chan struct{})
	close(done)

	handler := newPermissionHandler(approvals, done)
	out := handler(acp.PermissionRequest{
		Options: []acp.PermissionOption{{OptionID: "opt-allow", Kind: "allow"}},
	})
	if !out.Cancelled {
		t.Fatalf("got %+v, want cancelled when session done", out)
	}
}

func TestACPSessionCloseIdempotent(t *testing.T) {
	// We cannot build an acp.Client directly (newClient is unexported), so use a
	// minimal Backend compile check and exercise Close on a session whose client
	// field is nil by constructing it via the unexported constructor with a nil
	// client. Close clears handlers and closes channels once.
	s := newACPSession(nil, "sid-1")
	if err := s.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
	for range s.Events() {
		t.Fatal("closed events channel should not yield")
	}
	for range s.Approvals() {
		t.Fatal("closed approvals channel should not yield")
	}
}

func TestAcpCommandForOpencode(t *testing.T) {
	if _, _, err := acpCommandFor("opencode"); err == nil {
		t.Fatal("opencode must use serve")
	}
}

func TestACPBackendSmokeKimi(t *testing.T) {
	if os.Getenv("COCKPIT_SMOKE") != "1" {
		t.Skip("set COCKPIT_SMOKE=1 to run real-agent smoke")
	}
	if _, err := exec.LookPath("kimi"); err != nil {
		t.Skip("kimi not in PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	b := newACPBackend("kimi-cli")
	sess, err := b.Start(ctx, StartOpts{RepoDir: t.TempDir(), Seat: "kimi-smoke"})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.Close()

	go func() {
		_ = sess.Prompt(ctx, UserMessage{Text: "hi"})
	}()

	saw := false
	timeout := time.After(20 * time.Second)
	for !saw {
		select {
		case e := <-sess.Events():
			if e.Kind == EventMessage || e.Kind == EventUsage {
				saw = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for an ACP event")
		case <-ctx.Done():
			t.Fatalf("context done: %v", ctx.Err())
		}
	}
}

func TestACPBackendSmokeOpencode(t *testing.T) {
	if os.Getenv("COCKPIT_SMOKE") != "1" {
		t.Skip("set COCKPIT_SMOKE=1 to run real-agent smoke")
	}
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not in PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	b := newACPBackend("opencode")
	sess, err := b.Start(ctx, StartOpts{RepoDir: t.TempDir(), Seat: "opencode-smoke"})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.Close()

	go func() {
		_ = sess.Prompt(ctx, UserMessage{Text: "hi"})
	}()

	saw := false
	timeout := time.After(20 * time.Second)
	for !saw {
		select {
		case e := <-sess.Events():
			if e.Kind == EventMessage || e.Kind == EventUsage {
				saw = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for an ACP event")
		case <-ctx.Done():
			t.Fatalf("context done: %v", ctx.Err())
		}
	}
}

type fakeConfigurer struct {
	set     map[string]string
	modes   []string
	options []acp.ConfigOption
	err     error
}

func (f *fakeConfigurer) SetConfigOption(_ context.Context, _ acp.SessionID, id, v string) ([]acp.ConfigOption, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.set == nil {
		f.set = map[string]string{}
	}
	f.set[id] = v
	if f.options != nil {
		return f.options, nil
	}
	return []acp.ConfigOption{{ID: id, CurrentValue: v}}, nil
}
func (f *fakeConfigurer) SetMode(_ context.Context, _ acp.SessionID, m string) error {
	f.modes = append(f.modes, m)
	return f.err
}
func kimiLikeSession() acp.NewSessionResult {
	return acp.NewSessionResult{SessionID: "s1", ConfigOptions: []acp.ConfigOption{
		{ID: "model", Category: "model", CurrentValue: "kimi-code/k3-256k", Options: []acp.ConfigValue{{Value: "kimi-code/kimi-for-coding"}, {Value: "kimi-code/k3-256k"}}},
		{ID: "thinking", Category: "thought_level", CurrentValue: "high", Options: []acp.ConfigValue{{Value: "low"}, {Value: "high"}}},
	}, Modes: []acp.SessionMode{{ID: "default"}, {ID: "plan"}}, CurrentModeID: "default"}
}
func TestApplyACPOptsModelEffortReadOnly(t *testing.T) {
	f := &fakeConfigurer{}
	info, err := applyACPOpts(context.Background(), f, "s1", kimiLikeSession(), StartOpts{Model: "kimi-code/kimi-for-coding", Effort: "low", Access: AccessReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	if f.set["model"] != "kimi-code/kimi-for-coding" || f.set["thinking"] != "low" || !reflect.DeepEqual(f.modes, []string{"default"}) {
		t.Fatalf("set=%v modes=%v", f.set, f.modes)
	}
	if want := (SessionInfo{Model: "kimi-code/kimi-for-coding", Effort: "low", Access: "default"}); info != want {
		t.Fatalf("info=%+v", info)
	}
}
func TestApplyACPOptsUnknownModelFailsWithChoices(t *testing.T) {
	_, err := applyACPOpts(context.Background(), &fakeConfigurer{}, "s1", kimiLikeSession(), StartOpts{Model: "unavailable"})
	if !errors.Is(err, ErrModelUnavailable) || !strings.Contains(err.Error(), "kimi-code/kimi-for-coding") {
		t.Fatalf("err=%v", err)
	}
}
func TestApplyACPOptsNoModelOptionFails(t *testing.T) {
	res := kimiLikeSession()
	res.ConfigOptions = nil
	_, err := applyACPOpts(context.Background(), &fakeConfigurer{}, "s1", res, StartOpts{Model: "x"})
	if !errors.Is(err, ErrModelUnsupported) {
		t.Fatalf("err=%v", err)
	}
}
func TestApplyACPOptsDefaultAccessTouchesNothing(t *testing.T) {
	f := &fakeConfigurer{}
	info, err := applyACPOpts(context.Background(), f, "s1", kimiLikeSession(), StartOpts{})
	if err != nil || len(f.set) != 0 || len(f.modes) != 0 {
		t.Fatalf("err=%v set=%v modes=%v", err, f.set, f.modes)
	}
	if info != (SessionInfo{Model: "kimi-code/k3-256k", Effort: "high", Access: "default"}) {
		t.Fatalf("info=%+v", info)
	}
}
func TestApplyACPOptsWriteAndMissingPlanPreserveMode(t *testing.T) {
	for _, access := range []Access{AccessWrite, AccessReadOnly} {
		res := kimiLikeSession()
		res.Modes = nil
		f := &fakeConfigurer{}
		info, err := applyACPOpts(context.Background(), f, "s1", res, StartOpts{Access: access})
		if err != nil || len(f.modes) != 0 || info.Access != "default" {
			t.Fatalf("info=%+v err=%v modes=%v", info, err, f.modes)
		}
	}
}
func TestApplyACPOptsRejectsUnconfirmedSettings(t *testing.T) {
	for _, field := range []string{"model", "effort"} {
		t.Run(field, func(t *testing.T) {
			opts := StartOpts{Model: "kimi-code/kimi-for-coding"}
			if field == "effort" {
				opts = StartOpts{Effort: "low"}
			}
			for _, current := range []string{"", "unexpected"} {
				f := &fakeConfigurer{options: []acp.ConfigOption{{ID: "model", CurrentValue: current}, {ID: "thinking", CurrentValue: current}}}
				_, err := applyACPOpts(context.Background(), f, "s1", kimiLikeSession(), opts)
				if err == nil {
					t.Fatalf("accepted unconfirmed %s=%q", field, current)
				}
				if field == "model" && (!errors.Is(err, ErrModelUnavailable) || !strings.Contains(err.Error(), "kimi-code/k3-256k")) {
					t.Fatalf("err=%v", err)
				}
			}
		})
	}
}
func TestApplyACPOptsErrors(t *testing.T) {
	boom := errors.New("boom")
	for _, opts := range []StartOpts{{Model: "kimi-code/kimi-for-coding"}, {Effort: "low"}, {Access: AccessReadOnly}} {
		_, err := applyACPOpts(context.Background(), &fakeConfigurer{err: boom}, "s1", kimiLikeSession(), opts)
		if !errors.Is(err, boom) {
			t.Fatalf("err=%v", err)
		}
	}
	_, err := applyACPOpts(context.Background(), &fakeConfigurer{}, "s1", kimiLikeSession(), StartOpts{Effort: "extreme"})
	if err == nil {
		t.Fatal("accepted unsupported effort")
	}
}

func newACPSessionForTest(prompt func(context.Context, string) (acp.StopReason, error)) *acpSession {
	s := newACPSession(nil, "s1")
	s.promptFn = prompt
	return s
}

func TestACPPromptEmitsTurnEndEvenWhenBufferFull(t *testing.T) {
	testACPTerminalBuffer(t, nil, StateTurnCompleted)
}
func TestACPPromptErrorEmitsTurnFailed(t *testing.T) {
	testACPTerminalBuffer(t, errors.New("boom"), StateTurnFailed)
}
func testACPTerminalBuffer(t *testing.T, failure error, state string) {
	t.Helper()
	started := make(chan struct{})
	s := newACPSessionForTest(func(context.Context, string) (acp.StopReason, error) { close(started); return "end_turn", failure })
	defer s.Close()
	for i := 0; i < cap(s.events); i++ {
		s.events <- Event{Kind: EventMessage}
	}
	done := make(chan error, 1)
	go func() { done <- s.Prompt(context.Background(), UserMessage{Text: "x"}) }()
	<-started
	select {
	case err := <-done:
		t.Fatalf("prompt returned with full buffer: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	for i := 0; i < cap(s.events); i++ {
		<-s.events
	}
	select {
	case e := <-s.events:
		if e.Kind != EventState || e.State != state || (failure != nil && e.Err != "boom") || (failure == nil && e.Text != "end_turn") {
			t.Fatalf("event=%+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal event dropped")
	}
	if err := <-done; !errors.Is(err, failure) {
		t.Fatalf("err=%v", err)
	}
}
func TestACPInterruptCallsCancel(t *testing.T) {
	s := newACPSession(nil, "s1")
	defer s.Close()
	boom := errors.New("cancel failed")
	called := false
	s.cancelFn = func() error { called = true; return boom }
	if err := s.Interrupt(context.Background()); !called || !errors.Is(err, boom) {
		t.Fatalf("called=%v err=%v", called, err)
	}
}
func TestACPCloseUnblocksDispatch(t *testing.T) {
	s := newACPSessionForTest(func(context.Context, string) (acp.StopReason, error) { return "end_turn", nil })
	for i := 0; i < cap(s.events); i++ {
		s.events <- Event{Kind: EventMessage}
	}
	done := make(chan struct{})
	go func() { _ = s.Prompt(context.Background(), UserMessage{}); close(done) }()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock Prompt")
	}
	s.dispatchEvent(Event{Kind: EventState, State: StateTurnFailed})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

// These peers re-exec the test binary (including race instrumentation). On a
// low-core CI runner they compete with the other package binaries and git
// subprocesses; a five-second end-to-end budget also charged that scheduling
// delay to initialize/configure/prompt. Keep a bounded correctness-test budget
// with headroom for startup, exercised by the six-second delayed peer below.
const acpProcessTestTimeout = 30 * time.Second

// TestACPProcessHelper is a stdio ACP peer, launched only by the process tests.
func TestACPProcessHelper(t *testing.T) {
	if os.Getenv("COCKPIT_ACP_HELPER") != "1" {
		return
	}
	if os.Getenv("COCKPIT_ACP_SLOW_START") == "1" {
		time.Sleep(6 * time.Second)
	}
	fmt.Fprintln(os.Stderr, "acp-stderr:"+os.Getenv("COCKPIT_ACP_MARKER"))
	in := json.NewDecoder(os.Stdin)
	res := kimiLikeSession()
	mode := "default"
	cancelled := false
	var calls []string
	for {
		var m struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if in.Decode(&m) != nil {
			os.Exit(0)
		}
		var p map[string]any
		_ = json.Unmarshal(m.Params, &p)
		if m.Method == "session/cancel" {
			cancelled = true
			continue
		}
		if m.ID == nil {
			continue
		}
		calls = append(calls, m.Method)
		result := any(map[string]any{})
		switch m.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"loadSession": true}}
		case "session/new":
			result = map[string]any{"sessionId": "s1", "configOptions": res.ConfigOptions, "modes": map[string]any{"currentModeId": mode, "availableModes": res.Modes}}
		case "session/set_config_option":
			for i := range res.ConfigOptions {
				if res.ConfigOptions[i].ID == p["configId"] {
					res.ConfigOptions[i].CurrentValue = p["value"].(string)
				}
			}
			if os.Getenv("COCKPIT_ACP_WRONG_MODEL") == "1" {
				res.ConfigOptions[0].CurrentValue = "wrong"
			}
			result = map[string]any{"configOptions": res.ConfigOptions}
		case "session/set_mode":
			mode = p["modeId"].(string)
		case "session/prompt":
			cwd, _ := os.Getwd()
			text := fmt.Sprintf("%s|%s|%s|%s|%s|%v|%s", os.Getenv("COCKPIT_ACP_MARKER"), res.ConfigOptions[0].CurrentValue, res.ConfigOptions[1].CurrentValue, mode, cwd, cancelled, strings.Join(calls, ","))
			mustWriteJSON(os.Stdout, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s1", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}}}})
			result = map[string]any{"stopReason": "end_turn"}
		}
		mustWriteJSON(os.Stdout, map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
	}
}
func setupACPProcess(t *testing.T) {
	t.Helper()
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("KIMI_SHARE_DIR", t.TempDir())
	t.Setenv("COCKPIT_ACP_HELPER", "1")
	t.Setenv("COCKPIT_ACP_MARKER", "inherited")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	q := "'" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "'"
	script := "#!/bin/sh\nexec " + q + " -test.run='^TestACPProcessHelper$' -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "kimi"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}
func TestACPBackendOptionsReachProcess(t *testing.T) {
	setupACPProcess(t)
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprintf("resume=%v", resume), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), acpProcessTestTimeout)
			defer cancel()
			opts := StartOpts{RepoDir: t.TempDir(), Env: []string{"COCKPIT_ACP_MARKER=override"}, Model: "kimi-code/kimi-for-coding", Effort: "low", Access: AccessReadOnly}
			opts.RepoDir, _ = filepath.EvalSymlinks(opts.RepoDir)
			b := newACPBackend("kimi-cli")
			var sess Session
			var err error
			if resume {
				r, ok := b.(OptsResumer)
				if !ok {
					t.Fatal("ACP backend lacks ResumeWith")
				}
				sess, err = r.ResumeWith(ctx, "s1", opts)
			} else {
				sess, err = b.Start(ctx, opts)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			info, ok := sess.(InfoSession)
			if !ok {
				t.Fatal("ACP session lacks Info")
			}
			if got := info.Info(); got != (SessionInfo{Model: "kimi-code/kimi-for-coding", Effort: "low", Access: "default"}) {
				t.Fatalf("info=%+v", got)
			}
			if err := sess.Interrupt(ctx); err != nil {
				t.Fatal(err)
			}
			if err := sess.Prompt(ctx, UserMessage{Text: "probe"}); err != nil {
				t.Fatal(err)
			}
			select {
			case e := <-sess.Events():
				prefix := "override|kimi-code/kimi-for-coding|low|default|" + opts.RepoDir + "|true|"
				if e.Kind != EventMessage || !strings.HasPrefix(e.Text, prefix) {
					t.Fatalf("event=%+v", e)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			e := <-sess.Events()
			if e.State != StateTurnCompleted {
				t.Fatalf("event=%+v", e)
			}
		})
	}
}
func TestACPBackendStderrReachesWriter(t *testing.T) {
	setupACPProcess(t)
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprintf("resume=%v", resume), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), acpProcessTestTimeout)
			defer cancel()
			stderr, err := os.CreateTemp(t.TempDir(), "stderr")
			if err != nil {
				t.Fatal(err)
			}
			defer stderr.Close()
			opts := StartOpts{RepoDir: t.TempDir(), Env: []string{"COCKPIT_ACP_MARKER=stderr-capture"}, Stderr: stderr}
			b := newACPBackend("kimi-cli")
			var sess Session
			if resume {
				r, ok := b.(OptsResumer)
				if !ok {
					t.Fatal("ACP backend lacks ResumeWith")
				}
				sess, err = r.ResumeWith(ctx, "s1", opts)
			} else {
				sess, err = b.Start(ctx, opts)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			// The helper writes stderr before answering initialize, so both
			// session entry points must have captured it before returning.
			got, err := os.ReadFile(stderr.Name())
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "acp-stderr:stderr-capture\n" {
				t.Fatalf("captured stderr = %q, want %q", got, "acp-stderr:stderr-capture\n")
			}
		})
	}
}

func TestACPBackendDefaultPreservesRequests(t *testing.T) {
	setupACPProcess(t)
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprintf("resume=%v", resume), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), acpProcessTestTimeout)
			defer cancel()
			b := newACPBackend("kimi-cli")
			var sess Session
			var err error
			method := "session/new"
			if resume {
				sess, err = b.Resume(ctx, "s1")
				method = "session/load"
			} else {
				sess, err = b.Start(ctx, StartOpts{RepoDir: t.TempDir()})
			}
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			if err := sess.Prompt(ctx, UserMessage{Text: "probe"}); err != nil {
				t.Fatal(err)
			}
			e := <-sess.Events()
			want := "|false|initialize," + method + ",session/prompt"
			if !strings.HasSuffix(e.Text, want) {
				t.Fatalf("event=%+v", e)
			}
		})
	}
}
func TestACPBackendRejectsModelMismatch(t *testing.T) {
	setupACPProcess(t)
	t.Setenv("COCKPIT_ACP_WRONG_MODEL", "1")
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprintf("resume=%v", resume), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), acpProcessTestTimeout)
			defer cancel()
			b := newACPBackend("kimi-cli")
			opts := StartOpts{RepoDir: t.TempDir(), Model: "kimi-code/kimi-for-coding"}
			var sess Session
			var err error
			if resume {
				r, ok := b.(OptsResumer)
				if !ok {
					t.Fatal("ACP backend lacks ResumeWith")
				}
				sess, err = r.ResumeWith(ctx, "s1", opts)
			} else {
				sess, err = b.Start(ctx, opts)
			}
			if sess != nil {
				sess.Close()
				t.Fatal("usable session for wrong model")
			}
			if !errors.Is(err, ErrModelUnavailable) || !strings.Contains(err.Error(), "kimi-code/k3-256k") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

type modelChangingConfigurer struct{ fakeConfigurer }

func (f *modelChangingConfigurer) SetConfigOption(_ context.Context, _ acp.SessionID, id, value string) ([]acp.ConfigOption, error) {
	model := "kimi-code/kimi-for-coding"
	effort := "high"
	if id == "thinking" {
		model = "kimi-code/k3-256k"
		effort = value
	}
	res := kimiLikeSession()
	res.ConfigOptions[0].CurrentValue = model
	res.ConfigOptions[1].CurrentValue = effort
	return res.ConfigOptions, nil
}
func TestACPModelCannotChangeWhenSettingEffort(t *testing.T) {
	opts := StartOpts{Model: "kimi-code/kimi-for-coding", Effort: "low"}
	_, err := applyACPOpts(context.Background(), &modelChangingConfigurer{}, "s1", kimiLikeSession(), opts)
	if !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("start err=%v", err)
	}
	_, err = applyACPResumeOpts(context.Background(), &modelChangingConfigurer{}, "s1", opts)
	if !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("resume err=%v", err)
	}
}

// A busy runner can take longer than the old five-second budget to schedule a
// re-executed test binary. Hold the actual peer before initialize to reproduce
// that delay while still checking the full start/configure/prompt exchange.
func TestACPBackendOptionsReachSlowProcess(t *testing.T) {
	setupACPProcess(t)
	t.Setenv("COCKPIT_ACP_SLOW_START", "1")
	ctx, cancel := context.WithTimeout(context.Background(), acpProcessTestTimeout)
	defer cancel()
	sess, err := NewACPBackend("kimi-cli").Start(ctx, StartOpts{
		RepoDir: t.TempDir(), Model: "kimi-code/kimi-for-coding", Effort: "low", Access: AccessReadOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := sess.Prompt(ctx, UserMessage{Text: "probe"}); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-sess.Events():
		if e.Kind != EventMessage || !strings.Contains(e.Text, "|kimi-code/kimi-for-coding|low|default|") {
			t.Fatalf("event=%+v", e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestRemovedGeminiACP(t *testing.T) {
	if _, _, err := acpCommandFor("gemini-cli"); err == nil || !strings.Contains(err.Error(), "gemini-cli has been removed; use antigravity (agy) instead") {
		t.Errorf("error = %v", err)
	}
}
