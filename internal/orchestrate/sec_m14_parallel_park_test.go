package orchestrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/pact"
)

// Crash-safety regression — review finding M14.
//
// RunParallel parks the primary tree on pact-parallel-park and restored it with a
// bare `defer Checkout(base)`. A child goroutine panic / SIGKILL / OOM skips the
// defer → HEAD stays on the park branch, and the NEXT run captured that as base and
// landed every merge on it. RunSandbox already guards this with a park marker +
// checkStalePark; RunParallel now shares them.

func parallelParkOpts(t *testing.T, dir string) ParallelOptions {
	return ParallelOptions{
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
}

// A surviving park marker means a previous parallel run crashed before restoring
// the primary tree. RunParallel must REFUSE — naming the recorded branch — rather
// than re-park and read the park branch as base.
func TestRunParallel_RefusesStaleParkMarker(t *testing.T) {
	dir := newProject(t)
	if err := writeParkMarker(dir, "my-real-branch"); err != nil {
		t.Fatal(err)
	}
	err := RunParallel(context.Background(), parallelParkOpts(t, dir))
	if err == nil {
		t.Fatal("RunParallel ran over a stale park marker; want refusal")
	}
	if !strings.Contains(err.Error(), "my-real-branch") {
		t.Errorf("refusal does not name the recorded original branch: %v", err)
	}
	// Recovery is a deliberate human act — the refusal must not consume the marker.
	if _, serr := os.Stat(parkMarkerPath(dir)); serr != nil {
		t.Errorf("park marker was removed by the refusal: %v", serr)
	}
}

// A tree already sitting on the park branch (a pre-marker crash, or a manually
// deleted marker) must also refuse — CurrentBranch would otherwise become base.
func TestRunParallel_RefusesWhenTreeStillParked(t *testing.T) {
	dir := newProject(t)
	c := exec.Command("git", "checkout", "-q", "-b", "pact-parallel-park")
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("checkout park: %v %s", err, out)
	}
	err := RunParallel(context.Background(), parallelParkOpts(t, dir))
	if err == nil {
		t.Fatal("RunParallel re-parked a tree already on the park branch; want refusal")
	}
	if !strings.Contains(err.Error(), "pact-parallel-park") {
		t.Errorf("refusal does not mention the park branch: %v", err)
	}
}

// A CLEAN run must leave no debris: the primary tree is restored to base and the
// crash-guard marker is cleared, so the next run is not falsely refused.
func TestRunParallel_CleanRunLeavesNoParkMarker(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	writeSpec(t, dir, "tb", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	assignNoCheckout(t, dir, "tb", "fb", "feat-b", filepath.Join(".pact", "tasks", "tb.md"))
	gitCommitAll(t, dir, "assign fa+fb")
	base, _ := gitx.CurrentBranch(dir)

	if err := RunParallel(context.Background(), parallelParkOpts(t, dir)); err != nil {
		t.Fatalf("RunParallel: %v", err)
	}

	if after, _ := gitx.CurrentBranch(dir); after != base {
		t.Errorf("primary tree left on %q after a clean run, want restored to %q", after, base)
	}
	if _, err := os.Stat(parkMarkerPath(dir)); !os.IsNotExist(err) {
		t.Errorf("park marker survived a clean run (would falsely refuse the next run): %v", err)
	}
	// Sanity: both features shipped (the run really executed, not short-circuited).
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range st.Features {
		if f.Status != "shipped" {
			t.Errorf("feature %s = %q, want shipped", f.ID, f.Status)
		}
	}
}
