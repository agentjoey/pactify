package orchestrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// A surviving park marker means a previous sandbox run crashed before restoring
// the user's branch. RunSandbox must REFUSE — naming the recorded branch and the
// recovery commands — instead of re-parking, which would read pact-sandbox-park
// as "orig" and permanently lose the real branch.
func TestRunSandbox_RefusesStaleParkMarker(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")
	if err := writeParkMarker(dir, "my-real-branch"); err != nil {
		t.Fatal(err)
	}

	err := RunSandbox(context.Background(), sandboxOpts(t, dir))
	if err == nil {
		t.Fatal("RunSandbox ran over a stale park marker; want refusal")
	}
	if !strings.Contains(err.Error(), "my-real-branch") {
		t.Errorf("refusal does not name the recorded original branch: %v", err)
	}
	// The refusal must not consume the marker: recovery is a deliberate human act.
	if _, serr := os.Stat(parkMarkerPath(dir)); serr != nil {
		t.Errorf("park marker was removed by the refusal: %v", serr)
	}
}

// A tree already sitting on the park branch (a pre-marker crash, or a manually
// deleted marker) must also refuse — CurrentBranch would otherwise become "orig".
func TestRunSandbox_RefusesWhenTreeStillParked(t *testing.T) {
	dir := newProject(t)
	c := exec.Command("git", "checkout", "-q", "-b", parkBranch)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("checkout park: %v %s", err, out)
	}

	err := RunSandbox(context.Background(), sandboxOpts(t, dir))
	if err == nil {
		t.Fatal("RunSandbox re-parked a tree already on the park branch; want refusal")
	}
	if !strings.Contains(err.Error(), parkBranch) {
		t.Errorf("refusal does not mention the park branch: %v", err)
	}
}

// panicRunner simulates a driver-loop bug: the agent launch panics outright.
type panicRunner struct{}

func (panicRunner) Run(context.Context, LaunchContext) error { panic("runner exploded") }

// A panic inside the driver loop must still tear down: worktree removed, the
// user's branch restored (marker cleared), and the sandbox ledger copied back —
// while the panic itself propagates to the caller.
func TestRunSandbox_PanicStillRestoresAndCopiesBack(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")
	before := branchOf(t, dir)

	opts := sandboxOpts(t, dir)
	opts.Run = panicRunner{}

	recovered := func() (r any) {
		defer func() { r = recover() }()
		_ = RunSandbox(context.Background(), opts)
		return nil
	}()
	if recovered == nil {
		t.Fatal("expected the runner panic to propagate out of RunSandbox")
	}

	if _, err := os.Stat(filepath.Join(dir, ".pact", "orchestrate", "sandbox")); !os.IsNotExist(err) {
		t.Error("sandbox worktree survived the panic")
	}
	if after := branchOf(t, dir); after != before {
		t.Errorf("main tree left on %q after panic, want original %q", after, before)
	}
	if _, err := os.Stat(parkMarkerPath(dir)); !os.IsNotExist(err) {
		t.Error("park marker survived a successful restore")
	}
	// 回灌 happened: the driver recorded the task start in the SANDBOX ledger just
	// before launching the (panicking) worker; .pact is tracked here so the mid-run
	// mirror is skipped and only the deferred copy-back can carry it to the main dir.
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	status := ""
	for _, f := range st.Features {
		for _, task := range f.Tasks {
			if task.ID == "ta" {
				status = task.Status
			}
		}
	}
	if status != "in_progress" {
		t.Errorf("task ta status %q in main .pact after panic, want in_progress (ledger not copied back)", status)
	}
}
