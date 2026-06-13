package orchestrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// parFakeRunner drives the pact engine in whichever worktree it is launched in
// (repoDir), so two features advancing in two worktrees don't step on each other:
// worker brief → Checkpoint in repoDir; reviewer brief → Accept in repoDir.
type parFakeRunner struct{ t *testing.T }

func (f parFakeRunner) Run(_ context.Context, seatID, _, briefing, repoDir string) error {
	task := taskIDFromBrief(briefing)
	if task == "" {
		f.t.Fatalf("no task id in brief:\n%s", briefing)
	}
	if isWorker(briefing) {
		return pact.At(repoDir).As(seatID).Checkpoint(task, "evidence: tests pass")
	}
	return pact.At(repoDir).As(seatID).Accept(task)
}

// assignNoCheckout assigns a task (owner=w, reviewer=orch) acting as orch WITHOUT
// checking out the branch in the primary tree — RunParallel creates a worktree per
// feature, so the primary tree must stay on base.
func assignNoCheckout(t *testing.T, dir, taskID, feature, branch, spec string) {
	t.Helper()
	if err := pact.At(dir).As("orch").Assign(taskID, feature, branch, "w", "orch", spec, nil); err != nil {
		t.Fatalf("assign %s: %v", taskID, err)
	}
}

func gitCommitAll(t *testing.T, dir, msg string) {
	t.Helper()
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", msg}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
}

// Two independent features both ship under RunParallel: their workers/reviewers
// run in isolated worktrees concurrently, and the merges serialize onto base.
func TestRunParallel_TwoFeaturesBothShip(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	writeSpec(t, dir, "tb", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	assignNoCheckout(t, dir, "tb", "fb", "feat-b", filepath.Join(".pact", "tasks", "tb.md"))
	gitCommitAll(t, dir, "assign fa+fb") // commit to base so worktrees inherit

	popts := ParallelOptions{
		Options: Options{
			Dir:          dir,
			Th:           Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50},
			Run:          parFakeRunner{t: t},
			Exec:         &okExec{},
			Notify:       StdoutNotifier{},
			Now:          func() string { return "20260614-000000" },
			SeatKind:     func(string) string { return "claude-code" },
			Orchestrator: "orch",
		},
		MaxConcurrency: 2,
		WorktreeRoot:   filepath.Join(t.TempDir(), "wt"),
	}
	if err := RunParallel(context.Background(), popts); err != nil {
		t.Fatalf("RunParallel: %v", err)
	}

	// Back on base, both features are shipped.
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range st.Features {
		got[f.ID] = f.Status
	}
	if got["fa"] != "shipped" {
		t.Errorf("fa status = %q, want shipped", got["fa"])
	}
	if got["fb"] != "shipped" {
		t.Errorf("fb status = %q, want shipped", got["fb"])
	}
	// Worktrees are cleaned up.
	if entries, _ := os.ReadDir(popts.WorktreeRoot); len(entries) != 0 {
		t.Errorf("worktree root not cleaned: %v", entries)
	}
}

// MaxConcurrency<=1 falls back to the serial driver and still ships.
func TestRunParallel_SerialFallback(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "tc", "true")
	assign(t, dir, "tc", "fc", "feat-c", filepath.Join(".pact", "tasks", "tc.md"))

	popts := ParallelOptions{
		Options: Options{
			Dir:          dir,
			Th:           Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50},
			Run:          newFakeRunner(t, dir),
			Exec:         &okExec{},
			Notify:       StdoutNotifier{},
			Now:          func() string { return "20260614-000000" },
			SeatKind:     func(string) string { return "claude-code" },
			Orchestrator: "orch",
		},
		MaxConcurrency: 1,
	}
	if err := RunParallel(context.Background(), popts); err != nil {
		t.Fatalf("RunParallel serial: %v", err)
	}
	if s := featureStatus(t, dir, "fc"); s != "shipped" {
		t.Errorf("fc status = %q, want shipped under serial fallback", s)
	}
}
