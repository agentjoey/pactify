package orchestrate

import (
	"context"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// TestKindReReadsLiveRoster: opts.kind resolves a seat's kind from the LIVE
// roster on every call, so a seat that joins mid-run (or re-declares its kind via
// `pactify join --kind`) is drivable next iteration without a driver restart. The
// explicit --seat-kind override (SeatKind) still wins (spec §6 WS-K).
func TestKindReReadsLiveRoster(t *testing.T) {
	dir := newProject(t) // roster: orch (no kind), w (no kind)

	// No override → falls back to the live roster read.
	opts := Options{Dir: dir, SeatKind: func(string) string { return "" }}

	// Before any kind is declared, the roster carries none.
	if k := opts.kind("w"); k != "" {
		t.Fatalf("kind(w) before join = %q, want empty", k)
	}

	// Seat w re-joins declaring its kind — a mid-run roster mutation.
	if err := pact.At(dir).As("w").JoinKind("w", "worker", "opencode"); err != nil {
		t.Fatalf("JoinKind: %v", err)
	}

	// Re-read picks it up immediately (no restart, no rebuilt map).
	if k := opts.kind("w"); k != "opencode" {
		t.Fatalf("kind(w) after join = %q, want %q", k, "opencode")
	}

	// The explicit --seat-kind override still wins over the live roster.
	over := Options{Dir: dir, SeatKind: func(string) string { return "claude-code" }}
	if k := over.kind("w"); k != "claude-code" {
		t.Fatalf("override kind(w) = %q, want %q (flag must win)", k, "claude-code")
	}
}

// kindCapRunner is a fake runner that records the kind each launch was handed and,
// on the FIRST worker stint, joins a brand-new seat `late` (declaring its kind) and
// assigns it a dependent task — exercising a mid-run dynamic seat that must be
// drivable (with its kind) on a later iteration.
type kindCapRunner struct {
	dir    string
	kinds  map[string]string
	joined bool
}

func (r *kindCapRunner) Run(_ context.Context, lc LaunchContext) error {
	if isWorker(lc.Briefing) {
		// Record the kind handed to the OWNER launch (the reviewer stint would
		// otherwise overwrite it with the reviewer's kind).
		r.kinds[lc.Task] = lc.Kind
		if !r.joined {
			r.joined = true
			// `late` owns nothing yet → the join gate passes; declare its kind.
			if err := pact.At(r.dir).As("late").JoinKind("late", "worker", "opencode"); err != nil {
				return err
			}
			// Now give `late` a task that runs AFTER t1 (dep), created mid-run.
			if err := pact.At(r.dir).As("orch").Assign("t2", "f", "feat/x", "late", "orch", ".pact/tasks/t2.md", []string{"t1"}); err != nil {
				return err
			}
		}
		return pact.At(r.dir).As(lc.Seat).Checkpoint(lc.Task, "evidence")
	}
	return pact.At(r.dir).As(lc.Seat).Accept(lc.Task)
}

// TestLoopDrivesMidRunJoinedSeatWithItsKind: a seat that joins mid-run (with a
// declared kind) is launched with that kind on the iteration that reaches its task,
// proving the driver re-reads seat→kind from live state each iteration.
func TestLoopDrivesMidRunJoinedSeatWithItsKind(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "t1", "go test ./...")
	writeSpec(t, dir, "t2", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", ".pact/tasks/t1.md")

	runner := &kindCapRunner{dir: dir, kinds: map[string]string{}}
	// SeatKind nil → kind resolution comes purely from the live roster.
	opts := Options{
		Dir:    dir,
		Th:     Thresholds{MaxRework: 3, MaxFails: 2, MaxIters: 50},
		Run:    runner,
		Exec:   &okExec{},
		Notify: &recNotify{},
		Now:    fixedNow,
	}

	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// t1's owner `w` has no declared kind → launched with "" (control: the roster
	// read supplied late's kind, not a default).
	if got := runner.kinds["t1"]; got != "" {
		t.Fatalf("kind at t1 launch = %q, want empty", got)
	}
	// t2's owner `late` joined mid-run declaring opencode → launched with it.
	if got := runner.kinds["t2"]; got != "opencode" {
		t.Fatalf("kind at t2 launch = %q, want %q (mid-run join not re-read)", got, "opencode")
	}
	if s := featureStatus(t, dir, "f"); s != "shipped" {
		t.Fatalf("feature status = %q, want shipped", s)
	}
}
