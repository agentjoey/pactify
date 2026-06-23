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

// workRunner is a fake runner whose worker WRITES a file before checkpointing, so
// the feature branch actually diverges from base — a real base-advancing merge
// (the empty-tree parFakeRunner never exercises this).
type workRunner struct{ t *testing.T }

func (r workRunner) Run(_ context.Context, lc LaunchContext) error {
	task := taskIDFromBrief(lc.Briefing)
	if task == "" {
		r.t.Fatalf("no task id in brief:\n%s", lc.Briefing)
	}
	if isWorker(lc.Briefing) {
		if err := os.WriteFile(filepath.Join(lc.RepoDir, task+"-work.txt"), []byte("real work\n"), 0o644); err != nil {
			return err
		}
		return pact.At(lc.RepoDir).As(lc.Seat).Checkpoint(task, "evidence: tests pass")
	}
	return pact.At(lc.RepoDir).As(lc.Seat).Accept(task)
}

func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	c := exec.Command("git", "rev-parse", ref)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse %s: %v %s", ref, err, out)
	}
	return strings.TrimSpace(string(out))
}

func sandboxOpts(t *testing.T, dir string) Options {
	return Options{
		Dir:          dir,
		Th:           Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50},
		Run:          workRunner{t: t},
		Exec:         &okExec{},
		Notify:       StdoutNotifier{},
		Now:          func() string { return "20260623-000000" },
		SeatKind:     func(string) string { return "claude-code" },
		Orchestrator: "orch",
	}
}

// After a sandbox run ships a feature, the merged commit must land on the REAL
// project repo's base branch — base must advance and contain the feature work.
func TestRunSandbox_LandsMergeOnBase(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")

	base, _ := gitx.CurrentBranch(dir)
	before := revParse(t, dir, base)

	if err := RunSandbox(context.Background(), sandboxOpts(t, dir)); err != nil {
		t.Fatalf("RunSandbox: %v", err)
	}

	after := revParse(t, dir, base)
	if after == before {
		t.Fatalf("base %q did not advance after sandbox ship — merged commit not propagated to the real repo base", base)
	}
	if _, err := os.Stat(filepath.Join(dir, "ta-work.txt")); err != nil {
		t.Fatalf("feature work not present in the restored base tree: %v", err)
	}
}

// Same, but with an origin remote present — exercising the fetch-aware merge path
// (P3-3) inside the sandbox. The local base must still advance (sandbox never pushes).
func TestRunSandbox_LandsMergeOnBase_WithOrigin(t *testing.T) {
	dir := newProject(t)
	base, _ := gitx.CurrentBranch(dir)
	// wire a bare origin and push base, so DefaultBranch + fetch-aware merge engage
	bare := t.TempDir()
	for _, a := range [][]string{{"init", "--bare", "-q", bare}} {
		c := exec.Command("git", a...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	for _, a := range [][]string{{"remote", "add", "origin", bare}, {"push", "-q", "origin", base}, {"remote", "set-head", "origin", base}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}

	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")

	before := revParse(t, dir, base)
	if err := RunSandbox(context.Background(), sandboxOpts(t, dir)); err != nil {
		t.Fatalf("RunSandbox: %v", err)
	}
	after := revParse(t, dir, base)
	if after == before {
		t.Fatalf("base %q did not advance after sandbox ship (origin present) — merged commit not propagated to the real repo base", base)
	}
}
