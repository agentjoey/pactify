package orchestrate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
)

// fakeAcpConn is an in-process stand-in for *acp.Client that lets a test drive one
// stint: emit session updates (with usage), answer permission requests, and return
// a stop reason. It is the seam AcpRunner's Spawn injects, mirroring how CmdRunner
// tests inject a fake execFn (no real subprocess, no real JSON-RPC).
type fakeAcpConn struct {
	loadSession bool
	// prompt drives one turn: it may emit updates / request permission via the
	// fc helpers, then returns the stop reason (or an error).
	prompt func(fc *fakeAcpConn) (acp.StopReason, error)

	onUpdate func(acp.SessionUpdate)
	onPerm   func(acp.PermissionRequest) acp.PermissionOutcome

	closed    chan struct{}
	closeOnce sync.Once
}

func newFakeAcpConn() *fakeAcpConn { return &fakeAcpConn{closed: make(chan struct{})} }

func (f *fakeAcpConn) Initialize(context.Context) (acp.InitializeResult, error) {
	return acp.InitializeResult{ProtocolVersion: acp.ProtocolVersion, LoadSession: f.loadSession}, nil
}
func (f *fakeAcpConn) NewSession(context.Context, string) (acp.SessionID, error) {
	return "sess-1", nil
}
func (f *fakeAcpConn) Prompt(_ context.Context, _ acp.SessionID, _ string) (acp.StopReason, error) {
	if f.prompt != nil {
		return f.prompt(f)
	}
	return "end_turn", nil
}
func (f *fakeAcpConn) OnSessionUpdate(fn func(acp.SessionUpdate)) { f.onUpdate = fn }
func (f *fakeAcpConn) OnPermissionRequest(fn func(acp.PermissionRequest) acp.PermissionOutcome) {
	f.onPerm = fn
}
func (f *fakeAcpConn) Close() error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

// emitUpdate delivers a session update to the runner (as the real reader loop would).
func (f *fakeAcpConn) emitUpdate(u acp.SessionUpdate) {
	if f.onUpdate != nil {
		f.onUpdate(u)
	}
}

// requestPermission asks the runner's registered handler to answer a permission
// request and returns its outcome (as the real server→client request would).
func (f *fakeAcpConn) requestPermission(req acp.PermissionRequest) acp.PermissionOutcome {
	if f.onPerm != nil {
		return f.onPerm(req)
	}
	return acp.PermissionOutcome{Cancelled: true}
}

// captureSpawn returns a spawn seam that hands out fc and records the launch args.
func captureSpawn(fc *fakeAcpConn, cap *acpLaunch) acpSpawnFn {
	return func(_ context.Context, command string, args, env []string, dir string) (acpConn, error) {
		if cap != nil {
			cap.command, cap.args, cap.env, cap.dir = command, args, env, dir
		}
		return fc, nil
	}
}

type acpLaunch struct {
	command string
	args    []string
	env     []string
	dir     string
}

func (l acpLaunch) hasEnv(kv string) bool {
	for _, e := range l.env {
		if e == kv {
			return true
		}
	}
	return false
}

func TestAcpRunnerFullStintSuccess(t *testing.T) {
	fc := newFakeAcpConn()
	fc.prompt = func(fc *fakeAcpConn) (acp.StopReason, error) {
		fc.emitUpdate(acp.SessionUpdate{Kind: "agent_message_chunk"})
		return "end_turn", nil
	}
	var cap acpLaunch
	r := AcpRunner{Spawn: captureSpawn(fc, &cap)}
	err := r.Run(context.Background(), LaunchContext{Seat: "w", Kind: "kimi-cli", RepoDir: "/tmp/x"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cap.command != "kimi" || len(cap.args) != 1 || cap.args[0] != "acp" {
		t.Fatalf("kimi-cli should map to `kimi acp`, got %q %v", cap.command, cap.args)
	}
	if !cap.hasEnv("PACT_AGENT_ID=w") {
		t.Fatalf("PACT_AGENT_ID not injected: %v", cap.env)
	}
}

func TestAcpRunnerIdleKill(t *testing.T) {
	fc := newFakeAcpConn()
	fc.prompt = func(fc *fakeAcpConn) (acp.StopReason, error) {
		<-fc.closed // never any activity → block until the watchdog kills us
		return "", errors.New("connection closed")
	}
	r := AcpRunner{Spawn: captureSpawn(fc, nil), Idle: 40 * time.Millisecond}
	err := r.Run(context.Background(), LaunchContext{Seat: "w", Kind: "kimi-cli", RepoDir: "/tmp/x"})
	if !errors.Is(err, errIdle) {
		t.Fatalf("want errIdle, got %v", err)
	}
}

func TestAcpRunnerIdleResetByActivity(t *testing.T) {
	fc := newFakeAcpConn()
	fc.prompt = func(fc *fakeAcpConn) (acp.StopReason, error) {
		// Keep signaling activity within the idle window a few times, then finish.
		for i := 0; i < 4; i++ {
			fc.emitUpdate(acp.SessionUpdate{Kind: "tool_call"})
			time.Sleep(20 * time.Millisecond)
		}
		return "end_turn", nil
	}
	r := AcpRunner{Spawn: captureSpawn(fc, nil), Idle: 50 * time.Millisecond}
	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Kind: "kimi-cli", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("a busy agent must not be idle-killed: %v", err)
	}
}

func TestAcpRunnerPermissionAuto(t *testing.T) {
	fc := newFakeAcpConn()
	var got acp.PermissionOutcome
	fc.prompt = func(fc *fakeAcpConn) (acp.StopReason, error) {
		got = fc.requestPermission(acp.PermissionRequest{
			ToolCall: acp.PermissionToolCall{Title: "write file"},
			Options: []acp.PermissionOption{
				{OptionID: "r", Kind: "reject_once", Name: "Reject"},
				{OptionID: "a", Kind: "allow_once", Name: "Allow once"},
			},
		})
		return "end_turn", nil
	}
	r := AcpRunner{Spawn: captureSpawn(fc, nil), Policy: PermissionAuto}
	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Kind: "kimi-cli", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Cancelled || got.OptionID != "a" {
		t.Fatalf("auto policy should select the allow option, got %+v", got)
	}
}

func TestAcpRunnerPermissionDeny(t *testing.T) {
	fc := newFakeAcpConn()
	var got acp.PermissionOutcome
	fc.prompt = func(fc *fakeAcpConn) (acp.StopReason, error) {
		got = fc.requestPermission(acp.PermissionRequest{
			Options: []acp.PermissionOption{{OptionID: "a", Kind: "allow_once"}},
		})
		return "end_turn", nil
	}
	r := AcpRunner{Spawn: captureSpawn(fc, nil), Policy: PermissionDeny}
	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Kind: "kimi-cli", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !got.Cancelled {
		t.Fatalf("deny policy must cancel every request, got %+v", got)
	}
}

func TestAcpRunnerPermissionEscalate(t *testing.T) {
	dir := t.TempDir()
	fc := newFakeAcpConn()
	var got acp.PermissionOutcome
	fc.prompt = func(fc *fakeAcpConn) (acp.StopReason, error) {
		got = fc.requestPermission(acp.PermissionRequest{
			ToolCall: acp.PermissionToolCall{Title: "rm -rf /"},
			Options:  []acp.PermissionOption{{OptionID: "a", Kind: "allow_once"}},
		})
		return "end_turn", nil
	}
	r := AcpRunner{
		Spawn:  captureSpawn(fc, nil),
		Policy: PermissionEscalate,
		Now:    func() string { return "20260705-000000" },
	}
	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Task: "t1", Kind: "kimi-cli", RepoDir: dir}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !got.Cancelled {
		t.Fatalf("escalate policy must deny the request, got %+v", got)
	}
	path := filepath.Join(dir, ".pact", "orchestrate", "escalation-20260705-000000.md")
	b, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("escalate policy should write an escalation record: %v", rerr)
	}
	if !strings.Contains(string(b), "rm -rf /") {
		t.Fatalf("escalation should carry the requested action, got:\n%s", b)
	}
}

func TestAcpRunnerUsageCallback(t *testing.T) {
	fc := newFakeAcpConn()
	fc.prompt = func(fc *fakeAcpConn) (acp.StopReason, error) {
		fc.emitUpdate(acp.SessionUpdate{Kind: "usage", Usage: &acp.Usage{InputTokens: 100, OutputTokens: 40, Cost: 0.5}})
		fc.emitUpdate(acp.SessionUpdate{Kind: "usage", Usage: &acp.Usage{InputTokens: 20, OutputTokens: 10, Cost: 0.25}})
		return "end_turn", nil
	}
	var gotSeat, gotTask string
	var gotUsage acp.Usage
	var mu sync.Mutex
	fired := make(chan struct{}, 1)
	r := AcpRunner{
		Spawn: captureSpawn(fc, nil),
		OnUsage: func(seat, task string, u acp.Usage) {
			mu.Lock()
			gotSeat, gotTask, gotUsage = seat, task, u
			mu.Unlock()
			fired <- struct{}{}
		},
	}
	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Task: "t1", Kind: "kimi-cli", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("OnUsage callback never fired")
	}
	mu.Lock()
	defer mu.Unlock()
	if gotSeat != "w" || gotTask != "t1" {
		t.Fatalf("callback attribution wrong: seat=%q task=%q", gotSeat, gotTask)
	}
	if gotUsage.InputTokens != 120 || gotUsage.OutputTokens != 50 || gotUsage.Cost != 0.75 {
		t.Fatalf("usage not aggregated across updates: %+v", gotUsage)
	}
}

func TestAcpRunnerStripsAnthropicKey(t *testing.T) {
	fc := newFakeAcpConn()
	var cap acpLaunch
	r := AcpRunner{Spawn: captureSpawn(fc, &cap)}
	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Kind: "claude-code", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cap.command != "npx" {
		t.Fatalf("claude-code should map to an npx ACP adapter, got %q", cap.command)
	}
	if !cap.hasEnv("ANTHROPIC_API_KEY=") {
		t.Fatalf("claude-code ACP must strip ANTHROPIC_API_KEY, env=%v", cap.env)
	}
}

func TestAcpRunnerUnknownKindGuidesToCmd(t *testing.T) {
	r := AcpRunner{Spawn: captureSpawn(newFakeAcpConn(), nil)}
	err := r.Run(context.Background(), LaunchContext{Seat: "w", Kind: "opencode", RepoDir: "/tmp/x"})
	if err == nil {
		t.Fatal("opencode has no ACP transport — expected an error")
	}
	if !strings.Contains(err.Error(), "--transport") || !strings.Contains(err.Error(), "cmd") {
		t.Fatalf("error should point the user at --transport …=cmd, got %v", err)
	}
}

// recordRunner is a Runner that records which one ran (for routing tests).
type recordRunner struct {
	name string
	ran  *string
}

func (r recordRunner) Run(context.Context, LaunchContext) error {
	*r.ran = r.name
	return nil
}

func TestRoutedLocalRunnerRoutesByKind(t *testing.T) {
	var ran string
	rr := RoutedLocalRunner{
		Modes: map[string]string{"kimi-cli": "acp"},
		Cmd:   recordRunner{name: "cmd", ran: &ran},
		Acp:   recordRunner{name: "acp", ran: &ran},
	}
	if err := rr.Run(context.Background(), LaunchContext{Kind: "kimi-cli"}); err != nil {
		t.Fatal(err)
	}
	if ran != "acp" {
		t.Fatalf("kind mapped to acp should hit the ACP runner, ran=%q", ran)
	}
	if err := rr.Run(context.Background(), LaunchContext{Kind: "opencode"}); err != nil {
		t.Fatal(err)
	}
	if ran != "cmd" {
		t.Fatalf("unmapped kind should default to the command runner, ran=%q", ran)
	}
}

func TestRoutedLocalRunnerDefaultsToCmd(t *testing.T) {
	var ran string
	rr := RoutedLocalRunner{
		Cmd: recordRunner{name: "cmd", ran: &ran},
		Acp: recordRunner{name: "acp", ran: &ran},
	}
	if err := rr.Run(context.Background(), LaunchContext{Kind: "kimi-cli"}); err != nil {
		t.Fatal(err)
	}
	if ran != "cmd" {
		t.Fatalf("empty Modes must be all-cmd (zero behavior change), ran=%q", ran)
	}
}
