package orchestrate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// --- fix-until-green harness --------------------------------------------------

// flakyExec is a cmdExec that reports a RED gate for its first failFirst calls,
// then GREEN. It models a verify command that starts failing and goes green once
// the owner's fix lands — the injection point that drives the pre-review
// self-repair loop deterministically.
type flakyExec struct {
	calls     int
	failFirst int
}

func (e *flakyExec) Run(_ context.Context, _, _ string, _ map[string]string) (int, string, error) {
	e.calls++
	if e.calls <= e.failFirst {
		return 1, "FAIL: assertion failed in TestFoo", nil
	}
	return 0, "ok", nil
}

// fixRunner drives the pact engine for the fix-until-green tests. It recognizes
// THREE brief kinds: a fix-round brief ("pact fix round") re-runs the owner and
// checkpoints again; a worker brief checkpoints; a reviewer brief accepts. It
// counts each kind so tests can assert the self-repair re-ran the owner (not the
// reviewer) and never burned a failure. onFix, when set, fires as each fix brief
// arrives (used to capture the "fixing" status the driver emits just before).
type fixRunner struct {
	dir           string
	workerCalls   int
	fixCalls      int
	reviewerCalls int
	onFix         func()
}

func (r *fixRunner) Run(_ context.Context, lc LaunchContext) error {
	task := taskIDFromBrief(lc.Briefing)
	switch {
	case strings.Contains(lc.Briefing, "pact fix round"):
		r.fixCalls++
		if r.onFix != nil {
			r.onFix()
		}
		return pact.At(r.dir).As(lc.Seat).Checkpoint(task, "evidence: fixed")
	case isWorker(lc.Briefing):
		r.workerCalls++
		return pact.At(r.dir).As(lc.Seat).Checkpoint(task, "evidence: tests pass")
	default:
		r.reviewerCalls++
		return pact.At(r.dir).As(lc.Seat).Accept(task)
	}
}

// readStatus reads and decodes the runtime status.json under dir.
func readStatus(t *testing.T, dir string) Status {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".pact", "orchestrate", "status.json"))
	if err != nil {
		t.Fatalf("read status.json: %v", err)
	}
	var s Status
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("decode status.json: %v", err)
	}
	return s
}

// --- cases -------------------------------------------------------------------

// (a) RED → fix → GREEN: the driver re-runs the SAME owner on a failing gate,
// and once the gate goes green it proceeds to the reviewer (feature ships).
func TestFixLoopRedThenGreenProceedsToReview(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := &fixRunner{dir: dir}
	opts := baseOpts(dir, runner, &flakyExec{failFirst: 1}, &recNotify{})
	opts.MaxFixRounds = 2
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped (red→fix→green→review)", got)
	}
	if runner.fixCalls < 1 {
		t.Fatalf("owner not re-run on a red gate (fixCalls=%d, want >=1)", runner.fixCalls)
	}
	if runner.reviewerCalls < 1 {
		t.Fatalf("reviewer never ran after the gate went green (reviewerCalls=%d)", runner.reviewerCalls)
	}
}

// (b) rounds exhausted → escalate, with the last verify output carried in the
// escalation reason, and the reviewer never runs (a red gate must not reach it).
func TestFixLoopExhaustedEscalatesWithVerifyOutput(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := &fixRunner{dir: dir}
	notify := &recNotify{}
	opts := baseOpts(dir, runner, &failExec{}, notify)
	opts.MaxFixRounds = 2
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got == "shipped" {
		t.Fatal("feature shipped despite a gate that never goes green")
	}
	if runner.reviewerCalls != 0 {
		t.Fatalf("reviewer ran on a permanently-red gate (reviewerCalls=%d, want 0)", runner.reviewerCalls)
	}
	if runner.fixCalls != opts.MaxFixRounds {
		t.Fatalf("fix rounds = %d, want exactly MaxFixRounds=%d", runner.fixCalls, opts.MaxFixRounds)
	}

	esc := findEscalation(t, dir, fixedNow())
	b, err := os.ReadFile(esc)
	if err != nil {
		t.Fatalf("escalation file missing after exhausted fix rounds: %v", err)
	}
	body := string(b)
	if !strings.Contains(body, "fix-until-green exhausted") {
		t.Fatalf("escalation reason should name the exhausted self-repair loop:\n%s", body)
	}
	if !strings.Contains(body, "FAIL: assertion failed") {
		t.Fatalf("escalation reason should carry the last verify output:\n%s", body)
	}
	paused := false
	for _, m := range notify.msgs {
		if strings.Contains(m, "paused") {
			paused = true
		}
	}
	if !paused {
		t.Fatalf("expected a paused notification, got %v", notify.msgs)
	}
}

// (c) GREEN first try → straight to review, identical to the pre-WS-F flow: the
// owner is NOT re-run (no fix rounds), the reviewer runs, the feature ships.
func TestFixLoopGreenFirstTryGoesStraightToReview(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := &fixRunner{dir: dir}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped", got)
	}
	if runner.fixCalls != 0 {
		t.Fatalf("green-first-try must not re-run the owner (fixCalls=%d, want 0)", runner.fixCalls)
	}
	if runner.workerCalls != 1 {
		t.Fatalf("green-first-try must run the owner exactly once (workerCalls=%d, want 1)", runner.workerCalls)
	}
	if runner.reviewerCalls < 1 {
		t.Fatalf("reviewer never ran on a green first try (reviewerCalls=%d)", runner.reviewerCalls)
	}
}

// (d) fix rounds do NOT count toward MaxFails: with MaxFails=1 and a gate that
// needs two fix rounds to go green, the driver still ships (had fix rounds burned
// the failure budget, it would have escalated on the first round instead).
func TestFixLoopFixRoundsDoNotCountAsFails(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := &fixRunner{dir: dir}
	opts := baseOpts(dir, runner, &flakyExec{failFirst: 2}, &recNotify{})
	opts.MaxFixRounds = 2
	opts.Th.MaxFails = 1 // one real failure would escalate; fix rounds must not count
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped — fix rounds must not burn MaxFails", got)
	}
	if runner.fixCalls != 2 {
		t.Fatalf("expected 2 fix rounds before the gate went green, got %d", runner.fixCalls)
	}
}

// (status) the driver surfaces a `fixing` phase with round n/max while re-running
// the owner, so the board can show "fixing n/2".
func TestFixLoopStatusShowsFixingPhase(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := &fixRunner{dir: dir}
	var seen []Status
	runner.onFix = func() { seen = append(seen, readStatus(t, dir)) }
	opts := baseOpts(dir, runner, &flakyExec{failFirst: 1}, &recNotify{})
	opts.MaxFixRounds = 2
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(seen) == 0 {
		t.Fatal("no fixing status observed during the self-repair loop")
	}
	s := seen[0]
	if s.Phase != "fixing" {
		t.Fatalf("status phase = %q, want fixing", s.Phase)
	}
	if s.FixRound != 1 || s.FixMax != 2 {
		t.Fatalf("fixing round = %d/%d, want 1/2", s.FixRound, s.FixMax)
	}
	if s.Task != "t1" {
		t.Fatalf("fixing status task = %q, want t1", s.Task)
	}
	if s.Seat != "w" {
		t.Fatalf("fixing status seat = %q, want the owner w", s.Seat)
	}
}

// --- M16: fix-until-green budget resets across rework cycles ------------------

// redGate is verify state shared between the runner and the exec: the gate is
// RED while red==true. A fix round clears it (the fix went green); a rework
// re-reddens it (changes_requested re-introduces a failure). This makes each
// fix-until-green episode start RED regardless of how many times the gate is
// polled, so the test asserts on episode behavior, not call positions.
type redGate struct{ red bool }

type stateGateExec struct {
	g     *redGate
	calls int
}

func (e *stateGateExec) Run(_ context.Context, _, _ string, _ map[string]string) (int, string, error) {
	e.calls++
	if e.g.red {
		return 1, "FAIL: verify red", nil
	}
	return 0, "ok", nil
}

// reworkFixRunner drives BOTH the fix-until-green loop and a rework cycle: a fix
// brief re-checkpoints and greens the gate; a worker/rework brief checkpoints;
// the reviewer requests changes changesBeforeAccept times (re-reddening the gate)
// before accepting.
type reworkFixRunner struct {
	dir                 string
	g                   *redGate
	fixCalls            int
	reviewerCalls       int
	changesBeforeAccept int
	reviewSeen          map[string]int
}

func (r *reworkFixRunner) Run(_ context.Context, lc LaunchContext) error {
	task := taskIDFromBrief(lc.Briefing)
	switch {
	case strings.Contains(lc.Briefing, "pact fix round"):
		r.fixCalls++
		r.g.red = false // the self-repair round makes verify green
		return pact.At(r.dir).As(lc.Seat).Checkpoint(task, "evidence: fixed")
	case isWorker(lc.Briefing):
		return pact.At(r.dir).As(lc.Seat).Checkpoint(task, "evidence: tests pass")
	default: // reviewer
		r.reviewerCalls++
		n := r.reviewSeen[task]
		r.reviewSeen[task] = n + 1
		if n < r.changesBeforeAccept {
			r.g.red = true // the rework re-introduces a verify failure
			return pact.At(r.dir).As(lc.Seat).Changes(task, "rework the edge case")
		}
		return pact.At(r.dir).As(lc.Seat).Accept(task)
	}
}

// A rework cycle must reset the fix-until-green budget: after the reviewer
// requests changes and the owner re-checkpoints, the SECOND awaiting_review gets
// a fresh MaxFixRounds. With MaxFixRounds=1 and a gate that is red at the start
// of EACH episode, the feature ships only if the counter reset — otherwise round
// 2 sees round 1's count already at the cap and escalates with zero fix rounds
// (review finding M16). RED without the reset; GREEN with it.
func TestFixLoopBudgetResetsAcrossReworkCycles(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	g := &redGate{red: true} // verify starts red → episode 1 needs a fix round
	runner := &reworkFixRunner{dir: dir, g: g, changesBeforeAccept: 1, reviewSeen: map[string]int{}}
	opts := baseOpts(dir, runner, &stateGateExec{g: g}, &recNotify{})
	opts.MaxFixRounds = 1
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature = %q, want shipped — the fix-until-green budget must reset per rework cycle", got)
	}
	if runner.reviewerCalls < 2 {
		t.Fatalf("reviewer ran %d time(s), want >=2 (changes then accept)", runner.reviewerCalls)
	}
	if runner.fixCalls != 2 {
		t.Fatalf("fix rounds = %d, want 2 (one self-repair round in EACH episode)", runner.fixCalls)
	}
}
