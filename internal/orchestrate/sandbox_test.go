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

// A --sandbox run drives the feature to shipped in an isolated worktree, copies the
// advanced ledger back to the main .pact (回灌), and cleans up the worktree — all
// without leaving the main working tree on the scratch park branch.
func TestRunSandbox_ShipsAndCleansUp(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")

	before := branchOf(t, dir)

	opts := Options{
		Dir:          dir,
		Th:           Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50},
		Run:          parFakeRunner{t: t},
		Exec:         &okExec{},
		Notify:       StdoutNotifier{},
		Now:          func() string { return "20260621-000000" },
		SeatKind:     func(string) string { return "claude-code" },
		Orchestrator: "orch",
	}
	if err := RunSandbox(context.Background(), opts); err != nil {
		t.Fatalf("RunSandbox: %v", err)
	}

	// 回灌: the main .pact ledger now reflects the shipped feature.
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	shipped := false
	for _, f := range st.Features {
		if f.ID == "fa" && f.Status == "shipped" {
			shipped = true
		}
	}
	if !shipped {
		t.Fatalf("feature fa not shipped in main .pact after sandbox run: %+v", st.Features)
	}
	// the scratch worktree is gone and the main tree is back on its original branch.
	if _, err := os.Stat(filepath.Join(dir, ".pact", "orchestrate", "sandbox")); !os.IsNotExist(err) {
		t.Error("sandbox worktree was not removed")
	}
	if after := branchOf(t, dir); after != before {
		t.Errorf("main tree left on %q, want original %q (park not restored)", after, before)
	}
}

// A sandbox run must write its dashboard-observable runtime status (status.json,
// streams, escalation) to the MAIN repo dir, not the throwaway worktree — else
// serve, which watches <main>/.pact/orchestrate/status.json, sees no live progress
// (the worktree's copy is removed at teardown). Spec: coordination-authority P0b.
func TestRunSandbox_WritesStatusToMainDir(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")

	opts := Options{
		Dir:          dir,
		Th:           Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50},
		Run:          parFakeRunner{t: t},
		Exec:         &okExec{},
		Notify:       StdoutNotifier{},
		Now:          func() string { return "20260621-000000" },
		SeatKind:     func(string) string { return "claude-code" },
		Orchestrator: "orch",
	}
	if err := RunSandbox(context.Background(), opts); err != nil {
		t.Fatalf("RunSandbox: %v", err)
	}

	// status.json landed in the MAIN dir (survives worktree teardown).
	if _, err := os.Stat(filepath.Join(dir, ".pact", "orchestrate", "status.json")); err != nil {
		t.Fatalf("no status.json in main dir after sandbox run (dashboard would see nothing): %v", err)
	}
}

// mirrorLedger is the mid-run copy-back: while a sandboxed run advances the
// ledger in opts.Dir (the worktree), the board reads the MAIN dir, which would
// otherwise freeze on the seed until run end. mirrorLedger unions the sandbox
// ledger into the main dir each iteration so the board tracks live progress.
func TestMirrorLedger_UnionsSandboxIntoRuntimeDir(t *testing.T) {
	main := newProject(t) // the observed (runtime) dir
	if err := os.WriteFile(filepath.Join(main, ".gitignore"), []byte(".pact/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sandbox := t.TempDir() // stand-in for the worktree's .pact
	if err := os.MkdirAll(filepath.Join(sandbox, ".pact"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The sandbox ledger carries an event the main ledger has not seen yet.
	mainLog := filepath.Join(main, ".pact", "log.jsonl")
	seed, _ := os.ReadFile(mainLog)
	extra := []byte(`{"event_id":"cp1","ts":"2026-07-02T00:00:00Z","agent_id":"w","role":"worker","event_type":"checkpoint","task_id":"ta","feature":"fa","payload":{"evidence":"done"}}` + "\n")
	if err := os.WriteFile(filepath.Join(sandbox, ".pact", "log.jsonl"), append(append([]byte{}, seed...), extra...), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{Dir: sandbox, RuntimeDir: main}
	opts.mirrorLedger()

	merged, _ := os.ReadFile(mainLog)
	if !strings.Contains(string(merged), `"event_id":"cp1"`) {
		t.Fatalf("main ledger did not receive the sandbox checkpoint after mirror:\n%s", merged)
	}
}

// mirrorLedger must NOT write the runtime dir when it tracks .pact: doing so
// dirties the parked main tree and blocks teardown's branch restore — those
// repos keep only the run-end copy-back. It is also a no-op in-place.
func TestMirrorLedger_SkipsTrackedPactAndInPlace(t *testing.T) {
	main := newProject(t) // .pact is TRACKED here (no .gitignore)
	sandbox := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sandbox, ".pact"), 0o755); err != nil {
		t.Fatal(err)
	}
	mainLog := filepath.Join(main, ".pact", "log.jsonl")
	before, _ := os.ReadFile(mainLog)
	extra := []byte(`{"event_id":"cp2","ts":"2026-07-02T00:00:00Z","agent_id":"w","role":"worker","event_type":"checkpoint","task_id":"ta","feature":"fa","payload":{}}` + "\n")
	os.WriteFile(filepath.Join(sandbox, ".pact", "log.jsonl"), extra, 0o644)

	// Tracked .pact → skipped.
	Options{Dir: sandbox, RuntimeDir: main}.mirrorLedger()
	if after, _ := os.ReadFile(mainLog); string(after) != string(before) {
		t.Error("mirrorLedger wrote a runtime dir that tracks .pact (would dirty the parked tree)")
	}
	// In-place (RuntimeDir == Dir) → no-op regardless of ignore state.
	Options{Dir: main, RuntimeDir: main}.mirrorLedger()
	if after, _ := os.ReadFile(mainLog); string(after) != string(before) {
		t.Error("mirrorLedger mutated the ledger in the in-place case")
	}
}

// Real dogfooding sequence (2026-07-05 "P5"): drive a feature partway via
// --in-place (which really checks out the feature branch in the main tree as a
// side effect of driving each task), then switch to sandbox mode for the rest of
// the feature — a fresh `pactify orchestrate` invocation with fresh History. If
// sandbox mis-seeds or mis-enumerates the ledger for a feature that's already
// mid-flight on its own branch, this is where it would show up as total=0/done
// immediately instead of finding the remaining ready task.
func TestRunSandbox_AfterPartialInPlaceProgress(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "t1", "true")
	writeSpec(t, dir, "t2", "true")
	assignNoCheckout(t, dir, "t1", "fa", "feat-a", filepath.Join(".pact", "tasks", "t1.md"))
	if err := pact.At(dir).As("orch").Assign("t2", "fa", "feat-a", "w", "orch", filepath.Join(".pact", "tasks", "t2.md"), []string{"t1"}); err != nil {
		t.Fatalf("assign t2: %v", err)
	}
	gitCommitAll(t, dir, "assign fa (t1,t2)")

	// Partial --in-place progress: MaxIters=2 stops right after t1's owner+reviewer
	// iterations (checkpoint + accept), before t2 is ever touched.
	inPlaceOpts := Options{
		Dir: dir, Th: Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 2},
		Run: parFakeRunner{t: t}, Exec: &okExec{}, Notify: StdoutNotifier{},
		Now:          func() string { return "20260621-000000" },
		SeatKind:     func(string) string { return "claude-code" },
		Orchestrator: "orch",
	}
	if err := Run(context.Background(), inPlaceOpts); err != nil {
		t.Fatalf("in-place partial run: %v", err)
	}
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	var t1Status string
	for _, f := range st.Features {
		for _, tk := range f.Tasks {
			if tk.ID == "t1" {
				t1Status = tk.Status
			}
		}
	}
	if t1Status != "accepted" {
		t.Fatalf("setup: t1 should be accepted after partial in-place run, got %q (state: %+v)", t1Status, st.Features)
	}
	midBranch := branchOf(t, dir)
	if midBranch != "feat-a" {
		t.Fatalf("setup: main tree should be on feat-a after in-place progress, got %q", midBranch)
	}
	if dirty, _ := gitDirty(t, dir); dirty {
		t.Fatalf("setup: main tree is dirty after the in-place partial run (would block sandbox's park)")
	}

	// Switch to sandbox mode — fresh Options/History, as a real second CLI
	// invocation would load — to drive the rest of the feature (t2).
	sbOpts := Options{
		Dir: dir, Th: Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50},
		Run: parFakeRunner{t: t}, Exec: &okExec{}, Notify: StdoutNotifier{},
		Now:          func() string { return "20260621-000000" },
		SeatKind:     func(string) string { return "claude-code" },
		Orchestrator: "orch",
	}
	if err := RunSandbox(context.Background(), sbOpts); err != nil {
		t.Fatalf("RunSandbox after partial in-place progress: %v", err)
	}

	st2, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	shipped := false
	for _, f := range st2.Features {
		if f.ID != "fa" {
			continue
		}
		if f.Status == "shipped" {
			shipped = true
		}
		for _, tk := range f.Tasks {
			if tk.Status != "accepted" {
				t.Errorf("task %s not accepted after sandbox run: %+v", tk.ID, tk)
			}
		}
	}
	if !shipped {
		t.Fatalf("feature fa not shipped after sandbox drove the remaining task: %+v", st2.Features)
	}
	if after := branchOf(t, dir); after != midBranch {
		t.Errorf("main tree left on %q after sandbox teardown, want restored to %q", after, midBranch)
	}
}

func gitDirty(t *testing.T, dir string) (bool, error) {
	t.Helper()
	c := exec.Command("git", "status", "--porcelain")
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

func branchOf(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if i := len("ref: refs/heads/"); len(s) > i {
		return s[i : len(s)-1]
	}
	return s
}
