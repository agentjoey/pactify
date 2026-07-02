package orchestrate

import (
	"context"
	"os"
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
