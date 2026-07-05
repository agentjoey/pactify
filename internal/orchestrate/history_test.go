package orchestrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// escalationBody reads the escalation record written under dir (fixedNow
// timestamp). The filename also carries feature/task segments (P1) that vary
// by test, so this globs for the timestamp rather than hardcoding the exact
// name.
func escalationBody(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(findEscalation(t, dir, fixedNow()))
	if err != nil {
		t.Fatalf("escalation file missing: %v", err)
	}
	return string(b)
}

// findEscalation locates the (single) escalation file under dir whose name
// carries ts, regardless of the feature/task segments embedded before it
// (escalationFilename), and returns its path.
func findEscalation(t *testing.T, dir, ts string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".pact", "orchestrate", "escalation-*"+ts+".md"))
	if err != nil {
		t.Fatalf("glob escalation files: %v", err)
	}
	if len(matches) == 0 {
		entries, _ := os.ReadDir(filepath.Join(dir, ".pact", "orchestrate"))
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("no escalation file matching ts %q found; directory has: %v", ts, names)
	}
	if len(matches) > 1 {
		t.Fatalf("expected exactly one escalation file for ts %q, found %v", ts, matches)
	}
	return matches[0]
}

// Rework seeding: a ledger already carrying 2 changes_requested rounds for a
// task means h.Rework starts at 2, so ONE more rework in this run trips
// MaxRework=3 — a driver restart no longer grants a churning task a fresh
// rework budget.
func TestReworkSeededFromLedgerTripsAcrossRestart(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	// Two full review rounds recorded in the ledger BEFORE this driver starts
	// (as a previous driver's run would have left them).
	for i := 0; i < 2; i++ {
		if err := pact.At(dir).As("w").Checkpoint("t1", "evidence: round"); err != nil {
			t.Fatalf("checkpoint: %v", err)
		}
		if err := pact.At(dir).As("orch").Changes("t1", fmt.Sprintf("prior round %d", i+1)); err != nil {
			t.Fatalf("changes: %v", err)
		}
	}

	runner := newFakeRunner(t, dir)
	runner.alwaysChanges = true
	opts := baseOpts(dir, runner, &okExec{}, &recNotify{})
	opts.Th.MaxRework = 3
	notify := opts.Notify.(*recNotify)
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got == "shipped" {
		t.Fatal("feature shipped despite exceeding the cross-restart rework limit")
	}
	if !strings.Contains(escalationBody(t, dir), "rework limit exceeded") {
		t.Fatalf("escalation should name the rework limit:\n%s", escalationBody(t, dir))
	}
	// Seeding is what tripped it: only ONE review round ran in THIS process
	// (2 seeded + 1 observed = MaxRework 3). Without seeding, three in-run
	// rounds would have been needed.
	if runner.reviewerCalls != 1 {
		t.Fatalf("reviewer ran %d times, want 1 (rework budget seeded from ledger)", runner.reviewerCalls)
	}
	if len(notify.msgs) == 0 {
		t.Fatal("no escalation notification sent")
	}
}

// cancelSecondCallRunner crashes the worker on its first launch (a soft
// failure the driver records) and cancels the run context on the second —
// simulating a driver killed mid-run after one recorded failure.
type cancelSecondCallRunner struct {
	cancel context.CancelFunc
	calls  int
}

func (r *cancelSecondCallRunner) Run(ctx context.Context, _ LaunchContext) error {
	r.calls++
	if r.calls == 1 {
		return fmt.Errorf("simulated agent crash")
	}
	r.cancel()
	return ctx.Err()
}

// Fails persistence: run 1 records one soft failure and is cancelled; run 2
// (a restarted driver) loads that count and trips MaxFails=2 after a single
// further failure — instead of restarting the budget from zero.
func TestFailsPersistAcrossDriverRestart(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "false")
	assign(t, dir, "t1", "f", "feat/x", s1)

	// Run 1: one crash recorded, then the driver is cancelled mid-run.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r1 := &cancelSecondCallRunner{cancel: cancel}
	opts1 := baseOpts(dir, r1, &failExec{}, &recNotify{})
	opts1.Th.MaxFails = 2
	if err := Run(ctx, opts1); err == nil {
		t.Fatal("cancelled run should return an error")
	}

	// The failure count must have been persisted before the cancellation.
	if _, err := os.Stat(historyPath(dir, historyScopeAll)); err != nil {
		t.Fatalf("history file not persisted after a recorded failure: %v", err)
	}

	// Run 2: a fresh driver. The worker crashes again; with the loaded count the
	// SECOND overall failure trips MaxFails=2 — so the worker launches only once.
	r2 := &crashRunner{dir: dir, crashes: 99}
	opts2 := baseOpts(dir, r2, &failExec{}, &recNotify{})
	opts2.Th.MaxFails = 2
	notify := opts2.Notify.(*recNotify)
	if err := Run(context.Background(), opts2); err != nil {
		t.Fatalf("Run 2: %v", err)
	}

	if !strings.Contains(escalationBody(t, dir), "failure limit exceeded") {
		t.Fatalf("escalation should name the failure limit:\n%s", escalationBody(t, dir))
	}
	if r2.wcalls != 1 {
		t.Fatalf("run 2 launched the worker %d times, want 1 (fail budget loaded from history file)", r2.wcalls)
	}
	if len(notify.msgs) == 0 {
		t.Fatal("no escalation notification sent")
	}
	// The fired escalation clears the task's persisted budget, so a post-fix
	// rerun resumes with a fresh MaxFails instead of insta-tripping.
	h := History{Rework: map[string]int{}, Fails: map[string]int{}, LastFail: map[string]string{}}
	loadHistory(dir, historyScopeAll, &h)
	if h.Fails["t1"] != 0 {
		t.Fatalf("escalated task's persisted fail count = %d, want 0 (reset on trip)", h.Fails["t1"])
	}
}

// A run that reaches done must delete its history file — a completed run's
// failure counts must not poison the next run's thresholds.
func TestDoneRunClearsHistoryFile(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	// One crash first so a non-empty history file is written mid-run, then the
	// worker checkpoints and the reviewer accepts → shipped → done.
	run := &crashRunner{dir: dir, crashes: 1}
	if err := Run(context.Background(), baseOpts(dir, run, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped", got)
	}
	if _, err := os.Stat(historyPath(dir, historyScopeAll)); !os.IsNotExist(err) {
		t.Fatalf("history file survived a done run (stat err=%v); it must be deleted", err)
	}
}
