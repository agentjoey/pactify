package orchestrate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
	"github.com/agentjoey/pactify/internal/tokens"
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

	// newSessionID, when set, is the id NewSession returns (default "sess-1"); a
	// non-nil loadErr makes LoadSession fail (exercising the fall-back-to-New path).
	newSessionID string
	loadErr      error
	// newCalls / loadCalls record how many times each was invoked, and loadedID the
	// id LoadSession was last asked to resume — the resume tests assert on these.
	newCalls     int
	loadCalls    int
	loadedID     acp.SessionID
	promptedSID  acp.SessionID
	promptedText string

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
	f.newCalls++
	if f.newSessionID != "" {
		return acp.SessionID(f.newSessionID), nil
	}
	return "sess-1", nil
}
func (f *fakeAcpConn) LoadSession(_ context.Context, id acp.SessionID) error {
	f.loadCalls++
	f.loadedID = id
	return f.loadErr
}
func (f *fakeAcpConn) Prompt(_ context.Context, sid acp.SessionID, text string) (acp.StopReason, error) {
	f.promptedSID = sid
	f.promptedText = text
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
	path := findEscalation(t, dir, "20260705-000000")
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
		OnUsage: func(_ string, seat, task string, u acp.Usage) {
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

// TestAcpRunnerProductionRecordsTokens is the dm-acp-usage acceptance test: the
// PRODUCTION runner (NewAcpRunner wires OnUsage → recordAcpUsage) must fold an ACP
// stint's usage into the SAME per-task token store the CmdRunner path writes
// (internal/tokens at .pact/orchestrate/tokens.json). We inject a fake ACP conn
// that emits usage updates but keep the production OnUsage, run a stint against a
// temp RepoDir, then assert the store read API returns the summed input+output
// tokens keyed by task — the exact structure CmdRunner's recordTokens populates.
func TestAcpRunnerProductionRecordsTokens(t *testing.T) {
	dir := t.TempDir()
	fc := newFakeAcpConn()
	fc.prompt = func(fc *fakeAcpConn) (acp.StopReason, error) {
		fc.emitUpdate(acp.SessionUpdate{Kind: "usage", Usage: &acp.Usage{InputTokens: 100, OutputTokens: 40, Cost: 0.5}})
		fc.emitUpdate(acp.SessionUpdate{Kind: "usage", Usage: &acp.Usage{InputTokens: 20, OutputTokens: 10, Cost: 0.25}})
		return "end_turn", nil
	}
	// Production constructor wires OnUsage; swap only the spawn seam for the fake.
	r := NewAcpRunner(0, PermissionAuto)
	r.Spawn = captureSpawn(fc, nil)

	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Task: "t-acp", Kind: "kimi-cli", RepoDir: dir}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Same store, same read API, same on-disk file CmdRunner uses.
	s := tokens.Load(dir)
	if got := s.Get("t-acp"); got != 170 { // (100+40) + (20+10) = input 120 + output 50
		t.Fatalf("ACP stint should record summed input+output tokens into the shared store, got %d want 170", got)
	}
	if runs := s.Tasks["t-acp"].Runs; runs != 1 {
		t.Fatalf("one ACP stint should be one run, got %d", runs)
	}
	if _, err := os.Stat(filepath.Join(dir, ".pact", "orchestrate", "tokens.json")); err != nil {
		t.Fatalf("ACP usage must land in the CmdRunner store file .pact/orchestrate/tokens.json: %v", err)
	}
}

// TestAcpRunnerProductionZeroUsageNoRecord guards that a stint with no usage
// updates writes nothing (matching CmdRunner's unparseable-output no-op), so
// TOK stays absent rather than a phantom zero-run entry.
func TestAcpRunnerProductionZeroUsageNoRecord(t *testing.T) {
	dir := t.TempDir()
	fc := newFakeAcpConn()
	fc.prompt = func(fc *fakeAcpConn) (acp.StopReason, error) {
		fc.emitUpdate(acp.SessionUpdate{Kind: "agent_message_chunk"}) // no Usage
		return "end_turn", nil
	}
	r := NewAcpRunner(0, PermissionAuto)
	r.Spawn = captureSpawn(fc, nil)
	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Task: "t-zero", Kind: "kimi-cli", RepoDir: dir}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(tokens.Load(dir).Tasks) != 0 {
		t.Fatalf("a zero-usage stint must not write a token entry, got %d", len(tokens.Load(dir).Tasks))
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

// The ACP tier (kimi, gemini) invokes the native binary directly — NO npx.
func TestAcpCommandNativeBinariesForAcpTier(t *testing.T) {
	cases := []struct {
		kind, cmd string
		args      []string
	}{
		{"kimi-cli", "kimi", []string{"acp"}},
		{"gemini-cli", "gemini", []string{"--acp"}},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			command, args, ok := acpCommand(tc.kind)
			if !ok {
				t.Fatalf("acpCommand(%q): ok=false", tc.kind)
			}
			if command != tc.cmd {
				t.Errorf("command = %q, want %q (native binary, not npx)", command, tc.cmd)
			}
			if command == "npx" {
				t.Errorf("%s must NOT use npx — it has native ACP", tc.kind)
			}
			if !reflect.DeepEqual(args, tc.args) {
				t.Errorf("args = %v, want %v", args, tc.args)
			}
		})
	}
}

// The legacy/fallback deep-integration-tier bridges (claude, codex) stay pinned
// via npx (used only as a fallback; the real path is deep integration).
func TestAcpCommandPinsLegacyBridgeVersions(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{"claude-code", "@agentclientprotocol/claude-agent-acp@0.57.0"},
		{"codex-cli", "@zed-industries/codex-acp@0.16.0"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			command, args, ok := acpCommand(tc.kind)
			if !ok {
				t.Fatalf("acpCommand(%q): ok=false", tc.kind)
			}
			if command != "npx" {
				t.Errorf("command = %q, want npx", command)
			}
			found := false
			for _, a := range args {
				if a == tc.want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("args = %v, want to include %q", args, tc.want)
			}
		})
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

// TestAcpRunnerRecordsSessionOnNew is the baseline for resume: a fresh stint (no
// prior record) mints a new session and persists it, so a later retry can resume.
func TestAcpRunnerSessionRecordsOnNew(t *testing.T) {
	dir := t.TempDir()
	fc := newFakeAcpConn()
	fc.loadSession = true // capability present, but nothing to resume yet
	r := AcpRunner{Spawn: captureSpawn(fc, nil)}
	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Task: "t1", Kind: "kimi-cli", RepoDir: dir}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.newCalls != 1 || fc.loadCalls != 0 {
		t.Fatalf("first stint should NewSession, not LoadSession: new=%d load=%d", fc.newCalls, fc.loadCalls)
	}
	if got, ok := LookupSession(dir, "w", "t1"); !ok || got != "sess-1" {
		t.Fatalf("NewSession id should be persisted, got %q ok=%v", got, ok)
	}
	if strings.HasPrefix(fc.promptedText, "续接") {
		t.Fatalf("a fresh session must not carry the resume prefix, got %q", fc.promptedText)
	}
}

// TestAcpRunnerResumesWhenSupported: server advertises loadSession AND a prior
// record exists → LoadSession(oldID) is called first and the briefing is prefixed.
func TestAcpRunnerSessionResumesWhenSupported(t *testing.T) {
	dir := t.TempDir()
	if err := RecordSession(dir, "w", "t1", "kimi-cli", "old-sess"); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	fc := newFakeAcpConn()
	fc.loadSession = true
	r := AcpRunner{Spawn: captureSpawn(fc, nil)}
	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Task: "t1", Kind: "kimi-cli", RepoDir: dir, Briefing: "do the work"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.loadCalls != 1 || fc.loadedID != "old-sess" {
		t.Fatalf("retry should LoadSession the prior id first, load=%d id=%q", fc.loadCalls, fc.loadedID)
	}
	if fc.newCalls != 0 {
		t.Fatalf("a successful resume must not open a new session, new=%d", fc.newCalls)
	}
	if fc.promptedSID != "old-sess" {
		t.Fatalf("prompt should run against the resumed session, got %q", fc.promptedSID)
	}
	if !strings.HasPrefix(fc.promptedText, "续接上次会话") || !strings.Contains(fc.promptedText, "do the work") {
		t.Fatalf("resumed briefing should be prefixed then carry the original brief, got %q", fc.promptedText)
	}
}

// TestAcpRunnerNoResumeWhenUnsupported: a prior record exists but the server did
// NOT advertise loadSession → LoadSession is never called, falls to NewSession,
// briefing unprefixed (current behavior).
func TestAcpRunnerSessionNoResumeWhenUnsupported(t *testing.T) {
	dir := t.TempDir()
	if err := RecordSession(dir, "w", "t1", "kimi-cli", "old-sess"); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	fc := newFakeAcpConn()
	fc.loadSession = false
	r := AcpRunner{Spawn: captureSpawn(fc, nil)}
	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Task: "t1", Kind: "kimi-cli", RepoDir: dir, Briefing: "do the work"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.loadCalls != 0 {
		t.Fatalf("no loadSession capability → LoadSession must not be called, load=%d", fc.loadCalls)
	}
	if fc.newCalls != 1 {
		t.Fatalf("should fall back to NewSession, new=%d", fc.newCalls)
	}
	if strings.HasPrefix(fc.promptedText, "续接") {
		t.Fatalf("unsupported resume must not prefix the briefing, got %q", fc.promptedText)
	}
}

// TestAcpRunnerResumeFailureFallsBack: capability present, prior record present,
// but LoadSession errors (session expired/rejected) → the stale record is cleared
// and the stint falls back to a fresh NewSession, unprefixed.
func TestAcpRunnerSessionResumeFailureFallsBack(t *testing.T) {
	dir := t.TempDir()
	if err := RecordSession(dir, "w", "t1", "kimi-cli", "old-sess"); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	fc := newFakeAcpConn()
	fc.loadSession = true
	fc.loadErr = errors.New("session expired")
	r := AcpRunner{Spawn: captureSpawn(fc, nil)}
	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Task: "t1", Kind: "kimi-cli", RepoDir: dir, Briefing: "do the work"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.loadCalls != 1 {
		t.Fatalf("resume should be attempted once, load=%d", fc.loadCalls)
	}
	if fc.newCalls != 1 {
		t.Fatalf("a failed resume must fall back to NewSession, new=%d", fc.newCalls)
	}
	if strings.HasPrefix(fc.promptedText, "续接") {
		t.Fatalf("a failed resume runs a fresh session — no resume prefix, got %q", fc.promptedText)
	}
	// The stale id was cleared and replaced by the fresh NewSession id.
	if got, ok := LookupSession(dir, "w", "t1"); !ok || got != "sess-1" {
		t.Fatalf("record should now hold the fresh session id, got %q ok=%v", got, ok)
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

func TestAcpAuditRecordShellClass(t *testing.T) {
	lc := LaunchContext{Seat: "w", Kind: "kimi-cli", Task: "t1", Project: "p", RepoDir: "/tmp/x"}
	raw := []byte(`{"sessionUpdate":"tool_call","title":"run shell command","kind":"bash","rawInput":{"command":"ls -la"}}`)
	rec, ok := acpAuditRecord(lc, acp.SessionUpdate{SessionID: "s-1", Kind: "tool_call", Raw: raw}, time.Unix(1, 0).UTC())
	if !ok {
		t.Fatal("expected tool_call to be auditable")
	}
	if rec.Tool != "bash" {
		t.Errorf("tool = %q, want bash", rec.Tool)
	}
	if rec.Summary != "run shell command" {
		t.Errorf("summary = %q, want 'run shell command'", rec.Summary)
	}
	if rec.Risk != "exec" {
		t.Errorf("risk = %q, want exec", rec.Risk)
	}
	if rec.Kind != "acp:kimi-cli" {
		t.Errorf("kind = %q, want acp:kimi-cli", rec.Kind)
	}
	if rec.Session != "s-1" {
		t.Errorf("session = %q, want s-1", rec.Session)
	}
	if rec.Decision != "allow" {
		t.Errorf("decision = %q, want allow", rec.Decision)
	}
}

func TestAcpAuditRecordWriteClass(t *testing.T) {
	lc := LaunchContext{Seat: "w", Kind: "kimi-cli", Task: "t1", Project: "p", RepoDir: "/tmp/x"}
	raw := []byte(`{"sessionUpdate":"tool_call","title":"edit file","kind":"write_file","rawInput":{"path":"/tmp/x"}}`)
	rec, ok := acpAuditRecord(lc, acp.SessionUpdate{SessionID: "s-2", Kind: "tool_call", Raw: raw}, time.Unix(1, 0).UTC())
	if !ok {
		t.Fatal("expected tool_call to be auditable")
	}
	if rec.Tool != "write_file" {
		t.Errorf("tool = %q, want write_file", rec.Tool)
	}
	if rec.Summary != "edit file" {
		t.Errorf("summary = %q, want 'edit file'", rec.Summary)
	}
	if rec.Risk != "write" {
		t.Errorf("risk = %q, want write", rec.Risk)
	}
}

func TestAcpAuditRecordNoTitle(t *testing.T) {
	lc := LaunchContext{Seat: "w", Kind: "kimi-cli", Task: "t1", Project: "p", RepoDir: "/tmp/x"}
	raw := []byte(`{"sessionUpdate":"tool_call","kind":"read_file","rawInput":{"path":"/tmp/x"}}`)
	rec, ok := acpAuditRecord(lc, acp.SessionUpdate{SessionID: "s-3", Kind: "tool_call", Raw: raw}, time.Unix(1, 0).UTC())
	if !ok {
		t.Fatal("expected tool_call to be auditable")
	}
	if rec.Tool != "read_file" {
		t.Errorf("tool = %q, want read_file", rec.Tool)
	}
	if rec.Summary != "tool_call" {
		t.Errorf("summary = %q, want tool_call", rec.Summary)
	}
	if rec.Risk != "read" {
		t.Errorf("risk = %q, want read", rec.Risk)
	}
}

func TestAcpAuditRecordIgnoresToolCallUpdate(t *testing.T) {
	lc := LaunchContext{Seat: "w", Kind: "kimi-cli", Task: "t1", Project: "p", RepoDir: "/tmp/x"}
	raw := []byte(`{"sessionUpdate":"tool_call_update","title":"continue","kind":"bash"}`)
	_, ok := acpAuditRecord(lc, acp.SessionUpdate{SessionID: "s-4", Kind: "tool_call_update", Raw: raw}, time.Unix(1, 0).UTC())
	if ok {
		t.Fatal("expected tool_call_update to be ignored")
	}
}
