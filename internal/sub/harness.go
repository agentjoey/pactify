package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/orchestrate"
)

// DefaultHarness is used when the CLI's harness argument is omitted.
const DefaultHarness = "codex"

type Harness struct {
	TransientRetry bool // Prompt can send another turn on the same session.
	Name           string
	Kind           string
	Backend        func() cockpit.Backend
	Version        func(context.Context) (string, error)
	Verify         func(context.Context, VerificationInput) Verification
	// NoReadOnly, when set, is why this harness refuses --read-only jobs.
	NoReadOnly string
	// Experimental is an informational warning, independent of job status.
	Experimental string
	// MinimalConfig is supported only after a native, process-local path has
	// been verified. Unavailable harnesses retain user config and say why.
	MinimalConfig        bool
	Home                 *harnessHome
	VerificationBaseline func(context.Context, VerificationInput) (int64, error)
	LocalUsageBaseline   func(string, string) (int64, error)
	DoctorModel          string
	Models               func(context.Context, cockpit.StartOpts) ([]string, error)
	// Every harness must keep delegated sessions out of the user's own session
	// lists (TestEveryHarnessHidesItsSessions). HidesItself: the backend does it
	// (codex archives the thread on Close via StartOpts.Hidden). Otherwise Conceal
	// runs after the session has closed.
	HidesItself bool
	Conceal     func(ConcealContext) error
}

// VerificationInput bounds independent verification to this job's prompts.
type VerificationInput struct {
	Env        []string
	Cwd        string
	SessionID  string
	Info       cockpit.SessionInfo
	WireOffset int64 // Byte offset for kimi; exported message count for opencode.
	Prompts    int
}

type Verification struct {
	CostUSD       *float64
	Model         string
	InputTokens   int
	OutputTokens  int
	UsageRecorded bool // Independent token records are available, including zero usage.
	Verified      bool
	Warning       string
	UsagePartial  bool
}

// ConcealContext identifies the session a finished job leaves behind.
type ConcealContext struct {
	SessionID string
	Cwd       string
	Home      string // Actual harness home used by this job.
}

var registry = map[string]Harness{
	"agy": {
		TransientRetry: true, Name: "agy", Kind: "antigravity", Backend: cockpit.NewAgyBackend, Version: cliVersion("agy", orchestrate.CrossVendorStrip("antigravity")...),
		Experimental: "agy is an experimental harness (large tasks completed in one attempt 1/10; read-only work never converged)", MinimalConfig: true,
		Home:   &harnessHome{Env: "HOME", Source: os.UserHomeDir, Managed: managedAgyHome, Layout: agyHome, Doctor: doctorAgyHome, BeforeDoctor: snapshotAgyUserIndex},
		Models: cockpit.AgyModels, NoReadOnly: cockpit.AgyNoReadOnly,
		// Default jobs hide sessions through the managed HOME. User config explicitly
		// restores agy's session store; there is no supported archive API to hide it.
		Conceal: func(ConcealContext) error {
			return fmt.Errorf("agy --with-user-config keeps sessions in the user's session list; use minimal config to hide delegated sessions")
		},
		Verify: func(_ context.Context, v VerificationInput) Verification {
			return Verification{Model: v.Info.Model, Verified: v.Info.Model != ""}
		},
	},
	"opencode": {
		TransientRetry: true, Name: "opencode", Kind: "opencode", Backend: func() cockpit.Backend { return cockpit.NewACPBackend("opencode") },
		Version:      cliVersion("opencode", opencodeVendorStrip()...),
		Experimental: "opencode is an experimental harness (large tasks completed in one attempt 6/10; read-only work never converged)", MinimalConfig: true,
		Home: opencodeHomes(), Models: cockpit.OpencodeModels, NoReadOnly: cockpit.OpencodeNoReadOnly,
		Verify: verifyOpencode, VerificationBaseline: opencodeBaseline,
		Conceal: func(ConcealContext) error {
			return fmt.Errorf("opencode --with-user-config keeps sessions in the user's session list; use minimal config to hide delegated sessions")
		},
	},

	"codex": {
		DoctorModel:    "gpt-6-luna",
		TransientRetry: true, Name: "codex", Kind: "codex-cli", Backend: cockpit.NewCodexBackend, Version: cliVersion("codex"),
		HidesItself:   true,
		MinimalConfig: true,
		Verify: func(_ context.Context, v VerificationInput) Verification {
			return Verification{Model: v.Info.Model, Verified: v.Info.Model != ""}
		},
	},
	"kimi": {
		Home:               &harnessHome{Env: "KIMI_CODE_HOME", Source: kimiHomeDir, Managed: managedKimiHome, Layout: kimiHome, Doctor: doctorKimiHome},
		LocalUsageBaseline: cockpit.KimiWireOffset, DoctorModel: "kimi-code/kimi-for-coding",
		TransientRetry: true, Name: "kimi", Kind: "kimi-cli", Backend: func() cockpit.Backend { return cockpit.NewACPBackend("kimi-cli") }, Version: cliVersion("kimi"),
		Experimental:  "kimi is an experimental harness (Phase 0: large tasks completed in one attempt 6/10; read-only work never converged)",
		MinimalConfig: true,
		// Phase 0 (2026-10-15): in plan mode kimi keeps trying to write a plan file and
		// exit plan; all 3 read-only tasks hit the tool-call limit without findings.
		NoReadOnly: "Read-only access is not offered for kimi in the first release: all 3 Phase 0 read-only tasks hit the tool-call limit in plan mode without returning a conclusion. Use codex --read-only for read-only tasks; use kimi --write only with explicit write permission",
		Conceal:    concealKimi,
		Verify:     verifyKimiUsage,
	},
}

func (h Harness) configMode(withUserConfig bool) string {
	if h.MinimalConfig && !withUserConfig {
		return "minimal"
	}
	return "user"
}

// EffectiveConfigMode treats records from before AJ-636 as user config.
func (s State) EffectiveConfigMode() string {
	if s.ConfigMode == "" {
		return "user"
	}
	return s.ConfigMode
}

func Lookup(name string) (Harness, bool) {
	if name == "fake" && os.Getenv("PACTIFY_SUB_FAKE") == "1" {
		return fakeHarness(), true
	}
	h, ok := registry[name]
	return h, ok
}
func Names() []string {
	names := make([]string, 0, len(registry)+1)
	for name := range registry {
		names = append(names, name)
	}
	if os.Getenv("PACTIFY_SUB_FAKE") == "1" {
		names = append(names, "fake")
	}
	sort.Strings(names)
	return names
}
func cliVersion(bin string, env ...string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "--version")
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
		out, err := cmd.Output()
		if err != nil {
			return "", err
		}
		line, _, _ := strings.Cut(string(out), "\n")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return "", fmt.Errorf("%s --version returned no version", bin)
		}
		return fields[len(fields)-1], nil
	}
}

// The opt-in fixture exercises the detached CLI lifecycle without an installed agent.
func fakeHarness() Harness {
	return Harness{Name: "fake", HidesItself: true, Kind: "codex-cli", Backend: func() cockpit.Backend { return fixtureBackend{} }, Version: func(context.Context) (string, error) { return "test", nil }, Verify: func(_ context.Context, v VerificationInput) Verification {
		return Verification{Model: v.Info.Model, Verified: v.Info.Model != ""}
	}}
}

type fixtureBackend struct{}

func (fixtureBackend) Start(ctx context.Context, o cockpit.StartOpts) (cockpit.Session, error) {
	ctx, cancel := context.WithCancel(ctx)
	return &fixtureSession{ctx: ctx, cancel: cancel, opts: o, events: make(chan cockpit.Event, 4)}, nil
}
func (b fixtureBackend) Resume(ctx context.Context, _ string) (cockpit.Session, error) {
	return b.Start(ctx, cockpit.StartOpts{})
}
func (b fixtureBackend) ResumeWith(ctx context.Context, _ string, o cockpit.StartOpts) (cockpit.Session, error) {
	return b.Start(ctx, o)
}

type fixtureSession struct {
	ctx    context.Context
	cancel context.CancelFunc
	opts   cockpit.StartOpts
	events chan cockpit.Event
}

func (s *fixtureSession) Prompt(context.Context, cockpit.UserMessage) error {
	go func() {
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-s.ctx.Done():
			s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "interrupted"}
			return
		case <-timer.C:
		}
		if s.opts.Access == cockpit.AccessWrite {
			if err := os.WriteFile(filepath.Join(s.opts.RepoDir, "out.txt"), []byte("hi\n"), 0600); err != nil {
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: err.Error()}
				return
			}
		}
		s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 10, OutputTokens: 1}}
		s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start"}}
		s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "DONE"}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}()
	return nil
}
func (s *fixtureSession) Interrupt(context.Context) error           { s.cancel(); return nil }
func (s *fixtureSession) Events() <-chan cockpit.Event              { return s.events }
func (s *fixtureSession) Approvals() <-chan cockpit.ApprovalRequest { return nil }
func (s *fixtureSession) ThreadID() string                          { return "fixture-thread" }
func (s *fixtureSession) Info() cockpit.SessionInfo                 { return cockpit.SessionInfo{Model: s.opts.Model} }
func (s *fixtureSession) Close() error                              { s.cancel(); return nil }

// kimiHomeDir is kimi-code's data directory: KIMI_CODE_HOME, else ~/.kimi-code.
func kimiHomeDir() (string, error) {
	if h := os.Getenv("KIMI_CODE_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kimi-code"), nil
}

// concealKimi archives the job's kimi session through kimi's own server API, so the
// CLI, the web UI and Remote Control all hide it. If that path fails it falls back
// to kimi's on-disk archive fields (CLI lists only) and says so. Either way it then
// asks the official CLI whether the session is really hidden — kimi auto-updates,
// and a changed format must surface as a warning, not as silent clutter.
func concealKimi(c ConcealContext) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	home := c.Home
	archive := kimiServerArchive
	list := kimiListHides
	if home == "" {
		var err error
		home, err = kimiHomeDir()
		if err != nil {
			return err
		}
	} else {
		archive = func(ctx context.Context, sid string) error {
			return cockpit.ArchiveKimiSessionsViaServer(ctx, []string{sid}, home)
		}
		list = func(cwd, sid string) error { return kimiListHidesInHome(home, cwd, sid) }
	}
	apiErr := archive(ctx, c.SessionID)
	if apiErr != nil {
		if _, err := cockpit.ArchiveKimiSession(home, c.SessionID); err != nil {
			return fmt.Errorf("kimi archive API: %v; file fallback: %w", apiErr, err)
		}
	}
	if err := list(c.Cwd, c.SessionID); err != nil {
		return err
	}
	if apiErr != nil {
		return fmt.Errorf("Hidden from the kimi CLI session list, but may still appear in kimi web / Remote Control (official archive API failed: %v)", apiErr)
	}
	return nil
}

var kimiServerArchive = func(ctx context.Context, sessionID string) error {
	return cockpit.ArchiveKimiSessionsViaServer(ctx, []string{sessionID})
}

// kimiListHides fails only when `kimi session list` still shows the session; a CLI
// that cannot be run (not installed, timed out) is not evidence either way.
var kimiListHides = func(cwd, sessionID string) error { return kimiListHidesInHome("", cwd, sessionID) }

func kimiListHidesInHome(home, cwd, sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kimi", "session", "list", "--cwd", cwd, "--json")
	if home != "" {
		cmd.Env = append(os.Environ(), "KIMI_CODE_HOME="+home)
	}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var sessions []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(out, &sessions) != nil {
		return nil
	}
	for _, s := range sessions {
		if s.ID == sessionID {
			return fmt.Errorf("kimi session list still lists %s: this kimi version may have changed the session format", sessionID)
		}
	}
	return nil
}
