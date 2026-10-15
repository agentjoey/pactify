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
)

// DefaultHarness is used when the CLI's harness argument is omitted.
const DefaultHarness = "codex"

type Harness struct {
	Name    string
	Kind    string
	Backend func() cockpit.Backend
	Version func(context.Context) (string, error)
	Verify  func(string, cockpit.SessionInfo) (model string, in, out int, ok bool)
	// NoReadOnly, when set, is why this harness refuses --read-only jobs.
	NoReadOnly string
	// Experimental is an informational warning, independent of job status.
	Experimental string
	// Every harness must keep delegated sessions out of the user's own session
	// lists (TestEveryHarnessHidesItsSessions). HidesItself: the backend does it
	// (codex archives the thread on Close via StartOpts.Hidden). Otherwise Conceal
	// runs after the session has closed.
	HidesItself bool
	Conceal     func(ConcealContext) error
}

// ConcealContext identifies the session a finished job leaves behind.
type ConcealContext struct {
	SessionID string
	Cwd       string
}

var registry = map[string]Harness{
	"codex": {
		Name: "codex", Kind: "codex-cli", Backend: cockpit.NewCodexBackend, Version: cliVersion("codex"),
		HidesItself: true,
		Verify: func(_ string, info cockpit.SessionInfo) (string, int, int, bool) {
			return info.Model, 0, 0, info.Model != ""
		},
	},
	"kimi": {
		Name: "kimi", Kind: "kimi-cli", Backend: func() cockpit.Backend { return cockpit.NewACPBackend("kimi-cli") }, Version: cliVersion("kimi"),
		Experimental: "kimi 为实验性 harness（Phase 0：大任务一次完成 6/10，只读不收敛）",
		// Phase 0 (2026-10-15): in plan mode kimi keeps trying to write a plan file and
		// exit plan; all 3 read-only tasks hit the tool-call limit without findings.
		NoReadOnly: "kimi 的只读档在第一版不开放：Phase 0 实测 3 个只读任务在 plan 模式下全部打满工具调用上限、没有交回结论。只读任务请用 codex --read-only；用户明确允许写入时可用 kimi --write",
		Conceal:    concealKimi,
		Verify: func(sid string, _ cockpit.SessionInfo) (string, int, int, bool) {
			kimiHome, err := kimiHomeDir()
			if err != nil {
				return "", 0, 0, false
			}
			rec, err := cockpit.ReadKimiRecord(kimiHome, sid)
			if err != nil || len(rec.Models) == 0 {
				return "", 0, 0, false
			}
			return rec.Models[len(rec.Models)-1], rec.InputTokens, rec.OutputTokens, true
		},
	},
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
func cliVersion(bin string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, bin, "--version").Output()
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
	return Harness{Name: "fake", HidesItself: true, Kind: "codex-cli", Backend: func() cockpit.Backend { return fixtureBackend{} }, Version: func(context.Context) (string, error) { return "test", nil }, Verify: func(_ string, i cockpit.SessionInfo) (string, int, int, bool) { return i.Model, 0, 0, i.Model != "" }}
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
	apiErr := kimiServerArchive(ctx, c.SessionID)
	if apiErr != nil {
		home, err := kimiHomeDir()
		if err != nil {
			return err
		}
		if _, err := cockpit.ArchiveKimiSession(home, c.SessionID); err != nil {
			return fmt.Errorf("kimi archive API: %v; file fallback: %w", apiErr, err)
		}
	}
	if err := kimiListHides(c.Cwd, c.SessionID); err != nil {
		return err
	}
	if apiErr != nil {
		return fmt.Errorf("已从 kimi 命令行的会话列表隐藏，但 kimi web / Remote Control 可能仍可见（官方归档接口失败：%v）", apiErr)
	}
	return nil
}

var kimiServerArchive = func(ctx context.Context, sessionID string) error {
	return cockpit.ArchiveKimiSessionsViaServer(ctx, []string{sessionID})
}

// kimiListHides fails only when `kimi session list` still shows the session; a CLI
// that cannot be run (not installed, timed out) is not evidence either way.
var kimiListHides = func(cwd, sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "kimi", "session", "list", "--cwd", cwd, "--json").Output()
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
			return fmt.Errorf("kimi session list 仍列出 %s：当前 kimi 版本可能改了会话格式", sessionID)
		}
	}
	return nil
}
