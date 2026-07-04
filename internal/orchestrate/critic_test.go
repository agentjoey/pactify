package orchestrate

import (
	"context"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/projection"
)

// lastCriticNote returns the score/reason from the most recent critic note event
// for a task. Critic notes reuse the `start` event_type (no new event_type) and
// are distinguished by the `critic_by` payload key.
func lastCriticNote(t *testing.T, dir, task string) (score any, by, reason string) {
	t.Helper()
	evs, err := event.ReadAll(paths.LogIn(dir))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if e.EventType != "start" || e.TaskID != task {
			continue
		}
		if b, ok := e.Payload["critic_by"].(string); ok {
			r, _ := e.Payload["reason"].(string)
			return e.Payload["critic_score"], b, r
		}
	}
	t.Fatalf("no critic note found for task %q", task)
	return nil, "", ""
}

func mustState(t *testing.T, dir string) projection.State {
	t.Helper()
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	return st
}

func projSeat(id string) projection.Seat { return projection.Seat{ID: id} }

// --- score parse (pure) -------------------------------------------------------

func TestParseCriticScore(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name      string
		in        string
		wantScore *float64
		wantWhy   string
	}{
		{"normal with dash reason", "some notes\nCRITIC_SCORE: 0.4 — thin error handling", f(0.4), "thin error handling"},
		{"normal plain reason", "CRITIC_SCORE: 0.7 looks solid", f(0.7), "looks solid"},
		{"missing marker -> null", "just prose, no verdict line", nil, ""},
		{"bare marker no number -> null", "CRITIC_SCORE:", nil, ""},
		{"non-numeric after marker -> null", "CRITIC_SCORE: high risk", nil, ""},
		{"clamp above range", "CRITIC_SCORE: 1.5 too generous", f(1.0), "too generous"},
		{"clamp below range", "CRITIC_SCORE: -0.3 negative", f(0.0), "negative"},
		{"last marker wins", "CRITIC_SCORE: 0.1 old\nmore\nCRITIC_SCORE: 0.9 final", f(0.9), "final"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotScore, gotWhy := parseCriticScore(c.in)
			switch {
			case c.wantScore == nil && gotScore != nil:
				t.Fatalf("score = %v, want nil", *gotScore)
			case c.wantScore != nil && gotScore == nil:
				t.Fatalf("score = nil, want %v", *c.wantScore)
			case c.wantScore != nil && *gotScore != *c.wantScore:
				t.Fatalf("score = %v, want %v", *gotScore, *c.wantScore)
			}
			if gotWhy != c.wantWhy {
				t.Fatalf("reason = %q, want %q", gotWhy, c.wantWhy)
			}
		})
	}
}

// --- integration harness ------------------------------------------------------

// criticRunner drives the pact engine for the critic tests. A worker brief
// checkpoints; a critic brief writes a CRITIC_SCORE line to the task's stream
// (exactly where the production runner mirrors the agent's stdout) unless
// failCritic is set, in which case it returns an error to model a stint
// failure/timeout; a reviewer brief captures the reviewer briefing and accepts.
type criticRunner struct {
	dir          string
	scoreLine    string // written to the stream on a critic stint
	failCritic   bool   // critic stint returns an error (soft failure path)
	workerCalls  int
	criticCalls  int
	reviewCalls  int
	reviewBriefs []string
}

func (r *criticRunner) Run(_ context.Context, lc LaunchContext) error {
	task := taskIDFromBrief(lc.Briefing)
	switch {
	case strings.Contains(lc.Briefing, "pact critic"):
		r.criticCalls++
		if r.failCritic {
			return context.DeadlineExceeded // soft failure: driver must skip silently
		}
		if sink, err := OpenStreamSink(lc.streamDir(), task); err == nil {
			_, _ = sink.Write([]byte(r.scoreLine + "\n"))
			_ = sink.Close()
		}
		return nil
	case isWorker(lc.Briefing):
		r.workerCalls++
		return pact.At(r.dir).As(lc.Seat).Checkpoint(task, "evidence: tests pass")
	default:
		r.reviewCalls++
		r.reviewBriefs = append(r.reviewBriefs, lc.Briefing)
		return pact.At(r.dir).As(lc.Seat).Accept(task)
	}
}

// (1) critic configured → its score is injected into the reviewer briefing and
// recorded as a task note, and the feature still ships.
func TestCriticInjectsScoreIntoReviewerBrief(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)
	if err := pact.At(dir).As("orch").ConfigCritic("crit"); err != nil {
		t.Fatalf("config critic: %v", err)
	}

	runner := &criticRunner{dir: dir, scoreLine: "CRITIC_SCORE: 0.4 — 边界处理薄弱"}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped", got)
	}
	if runner.criticCalls != 1 {
		t.Fatalf("critic stints = %d, want exactly 1", runner.criticCalls)
	}
	if len(runner.reviewBriefs) == 0 {
		t.Fatal("reviewer never ran")
	}
	rb := runner.reviewBriefs[0]
	if !strings.Contains(rb, "critic pre-review 0.4") {
		t.Fatalf("reviewer brief missing injected critic score:\n%s", rb)
	}
	if !strings.Contains(rb, "边界处理薄弱") {
		t.Fatalf("reviewer brief missing critic reason:\n%s", rb)
	}
	if !strings.Contains(rb, "focus your check here") {
		t.Fatalf("reviewer brief missing focus cue:\n%s", rb)
	}

	// The score was recorded as a task note (start-typed event, no new event_type).
	if _, _, reason := lastCriticNote(t, dir, "t1"); reason != "边界处理薄弱" {
		t.Fatalf("critic note reason = %q, want the parsed reason", reason)
	}
}

// (2) critic score has NO gating power: a low score never bounces the task — the
// reviewer still runs and the feature ships exactly as it would without a critic.
func TestCriticLowScoreDoesNotBounce(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)
	if err := pact.At(dir).As("orch").ConfigCritic("crit"); err != nil {
		t.Fatalf("config critic: %v", err)
	}

	runner := &criticRunner{dir: dir, scoreLine: "CRITIC_SCORE: 0.0 — 很差"}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped — a low critic score must not gate", got)
	}
	if runner.reviewCalls != 1 {
		t.Fatalf("reviewer runs = %d, want 1 (low score neither bounces nor re-reviews)", runner.reviewCalls)
	}
}

// (3) critic stint failure/timeout is skipped silently: the flow proceeds to the
// reviewer and the feature ships (no injection, no note, never blocks).
func TestCriticFailureDoesNotBlock(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)
	if err := pact.At(dir).As("orch").ConfigCritic("crit"); err != nil {
		t.Fatalf("config critic: %v", err)
	}

	runner := &criticRunner{dir: dir, failCritic: true}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped — a critic failure must not block", got)
	}
	if runner.criticCalls != 1 {
		t.Fatalf("critic stints = %d, want 1 (attempted once, then skipped)", runner.criticCalls)
	}
	if runner.reviewCalls != 1 {
		t.Fatalf("reviewer runs = %d, want 1 (flow proceeds despite critic failure)", runner.reviewCalls)
	}
	if strings.Contains(runner.reviewBriefs[0], "critic pre-review") {
		t.Fatalf("failed critic must inject nothing:\n%s", runner.reviewBriefs[0])
	}
}

// (4) default (no critic configured) → zero extra stints, reviewer briefing
// byte-identical to the no-critic build.
func TestCriticDefaultOffByteIdentical(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := &criticRunner{dir: dir}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped", got)
	}
	if runner.criticCalls != 0 {
		t.Fatalf("critic stints = %d, want 0 (feature OFF by default)", runner.criticCalls)
	}
	if len(runner.reviewBriefs) == 0 {
		t.Fatal("reviewer never ran")
	}
	got := runner.reviewBriefs[0]
	if strings.Contains(got, "critic 预评") || strings.Contains(got, "critic pre-review") {
		t.Fatalf("default-off reviewer brief leaked a critic section:\n%s", got)
	}
	// Byte-identical to a reviewer brief built with an empty critic note.
	seat := projSeat("orch")
	_, task, ok := find(mustState(t, dir), "f", "t1")
	if !ok {
		t.Fatal("task t1 not found in state")
	}
	if want := reviewerBrief(dir, seat, task, ""); got != want {
		t.Fatalf("default-off reviewer brief not byte-identical.\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}
