package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// drainFakeRunner makes feature fa's driver error out FIRST while feature fb is
// still in flight, so fb's result arrives on the coordinator's drain path:
//   - ta (fa's task): corrupt fa's worktree ledger so the post-run reprojection
//     fails (driveFeature returns err → dispatchErr), then unblock fb;
//   - tb (fb's tasks): the worker blocks until fa has errored, then checkpoints;
//     the reviewer accepts — fb comes back mergeable AFTER dispatchErr is set.
type drainFakeRunner struct {
	t        *testing.T
	unblockB chan struct{}
	once     sync.Once
}

func (f *drainFakeRunner) Run(_ context.Context, lc LaunchContext) error {
	seatID, briefing, repoDir := lc.Seat, lc.Briefing, lc.RepoDir
	task := taskIDFromBrief(briefing)
	if task == "" {
		f.t.Fatalf("no task id in brief:\n%s", briefing)
	}
	if task == "ta" {
		log := filepath.Join(repoDir, ".pact", "log.jsonl")
		if err := os.WriteFile(log, []byte("not json\n"), 0o644); err != nil {
			f.t.Errorf("corrupt fa ledger: %v", err)
		}
		f.once.Do(func() { close(f.unblockB) })
		return nil // clean exit; the reprojection error is what kills fa's driver
	}
	if isWorker(briefing) {
		<-f.unblockB
		return pact.At(repoDir).As(seatID).Checkpoint(task, "evidence: tests pass")
	}
	return pact.At(repoDir).As(seatID).Accept(task)
}

// One feature's error must not silently abandon a sibling that finished its own
// work: fa's driver errors (setting dispatchErr) while fb is still in flight, and
// fb — draining as mergeable — must still be merged and marked shipped, not just
// have its worktree discarded.
func TestRunParallel_DrainMergesSiblingAfterError(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	writeSpec(t, dir, "tb", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	assignNoCheckout(t, dir, "tb", "fb", "feat-b", filepath.Join(".pact", "tasks", "tb.md"))
	gitCommitAll(t, dir, "assign fa+fb")

	popts := ParallelOptions{
		Options: Options{
			Dir:          dir,
			Th:           Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50},
			Run:          &drainFakeRunner{t: t, unblockB: make(chan struct{})},
			Exec:         &okExec{},
			Notify:       StdoutNotifier{},
			Now:          func() string { return "20260702-000000" },
			SeatKind:     func(string) string { return "claude-code" },
			Orchestrator: "orch",
		},
		MaxConcurrency: 2,
		WorktreeRoot:   filepath.Join(t.TempDir(), "wt"),
	}
	err := RunParallel(context.Background(), popts)
	if err == nil {
		t.Fatal("RunParallel: want fa's reprojection error, got nil")
	}

	// Back on base: fb shipped despite fa's error; fa did not.
	st, perr := pact.At(dir).StateProjection()
	if perr != nil {
		t.Fatal(perr)
	}
	got := map[string]string{}
	for _, f := range st.Features {
		got[f.ID] = f.Status
	}
	if got["fb"] != "shipped" {
		t.Errorf("fb status = %q, want shipped (drained mergeable feature was dropped)", got["fb"])
	}
	if got["fa"] == "shipped" {
		t.Errorf("fa status = shipped, want unshipped (its driver errored)")
	}
	// The drain path still cleans up both worktrees.
	if entries, _ := os.ReadDir(popts.WorktreeRoot); len(entries) != 0 {
		t.Errorf("worktree root not cleaned: %v", entries)
	}
}
