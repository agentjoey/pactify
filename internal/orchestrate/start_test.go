package orchestrate

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// startObservingRunner checkpoints/accepts like parFakeRunner, but when launched
// as the WORKER it first snapshots the task's projected status — the board's
// view at the moment the owner is working.
type startObservingRunner struct {
	t        *testing.T
	atLaunch *string // task status observed when the worker was launched
}

func (f startObservingRunner) Run(_ context.Context, lc LaunchContext) error {
	task := taskIDFromBrief(lc.Briefing)
	if task == "" {
		f.t.Fatalf("no task id in brief:\n%s", lc.Briefing)
	}
	if isWorker(lc.Briefing) {
		if st, err := pact.At(lc.RepoDir).StateProjection(); err == nil {
			if _, tk, ok := find(st, task, task); ok {
				*f.atLaunch = tk.Status
			}
		}
		return pact.At(lc.RepoDir).As(lc.Seat).Checkpoint(task, "evidence: tests pass")
	}
	return pact.At(lc.RepoDir).As(lc.Seat).Accept(task)
}

// The driver records a task-scoped `start` before launching the owner, so the
// board shows in_progress WHILE the worker works — not `assigned` until the
// checkpoint lands (the "kimi working but stuck on assigned" gap).
func TestRunEmitsStartSoOwnerWorksAsInProgress(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assign(t, dir, "ta", "ta", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))

	var atLaunch string
	opts := Options{
		Dir:          dir,
		Th:           Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50},
		Run:          startObservingRunner{t: t, atLaunch: &atLaunch},
		Exec:         &okExec{},
		Notify:       StdoutNotifier{},
		Now:          func() string { return "20260702-000000" },
		SeatKind:     func(string) string { return "claude-code" },
		Orchestrator: "orch",
	}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if atLaunch != "in_progress" {
		t.Fatalf("task status while the owner worked = %q, want in_progress (start not recorded before launch)", atLaunch)
	}
}
