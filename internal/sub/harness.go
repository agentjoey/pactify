package sub

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

type Harness struct {
	Name    string
	Kind    string
	Backend func() cockpit.Backend
	Version func(context.Context) (string, error)
	Verify  func(string, cockpit.SessionInfo) (model string, in, out int, ok bool)
	// NoReadOnly, when set, is why this harness refuses --read-only jobs.
	NoReadOnly string
}

var registry = map[string]Harness{
	"codex": {
		Name: "codex", Kind: "codex-cli", Backend: cockpit.NewCodexBackend, Version: cliVersion("codex"),
		Verify: func(_ string, info cockpit.SessionInfo) (string, int, int, bool) {
			return info.Model, 0, 0, info.Model != ""
		},
	},
	"kimi": {
		Name: "kimi", Kind: "kimi-cli", Backend: func() cockpit.Backend { return cockpit.NewACPBackend("kimi-cli") }, Version: cliVersion("kimi"),
		// Phase 0 (2026-10-15): in plan mode kimi keeps trying to write a plan file and
		// exit plan; all 3 read-only tasks hit the tool-call limit without findings.
		NoReadOnly: "kimi 的只读档在第一版不开放：Phase 0 实测 3 个只读任务在 plan 模式下全部打满工具调用上限、没有交回结论。只读任务请用 codex --read-only；用户明确允许写入时可用 kimi --write",
		Verify: func(sid string, _ cockpit.SessionInfo) (string, int, int, bool) {
			kimiHome := os.Getenv("KIMI_CODE_HOME")
			if kimiHome == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return "", 0, 0, false
				}
				kimiHome = filepath.Join(home, ".kimi-code")
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
	return Harness{Name: "fake", Kind: "codex-cli", Backend: func() cockpit.Backend { return fixtureBackend{} }, Version: func(context.Context) (string, error) { return "test", nil }, Verify: func(_ string, i cockpit.SessionInfo) (string, int, int, bool) { return i.Model, 0, 0, i.Model != "" }}
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
