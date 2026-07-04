package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
)

// --- score/verdict parse (pure) ----------------------------------------------

func TestParseQAResult(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    qaOutcome
		wantWhy string
	}{
		{"pass with colon reason", "ran the app\nQA_RESULT: PASS: login works", qaPass, "login works"},
		{"fail with dash reason", "QA_RESULT: FAIL — 500 on submit", qaFail, "500 on submit"},
		{"pass no reason", "QA_RESULT: PASS", qaPass, ""},
		{"missing marker -> lenient", "no verdict line here", qaMissing, ""},
		{"marker no verdict -> lenient", "QA_RESULT: unsure", qaMissing, ""},
		{"bare marker -> lenient", "QA_RESULT:", qaMissing, ""},
		{"last marker wins", "QA_RESULT: FAIL: old\nmore\nQA_RESULT: PASS: fixed", qaPass, "fixed"},
		{"case-insensitive verdict", "qa_result: pass: ok", qaMissing, ""}, // marker is case-sensitive prefix
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, why := parseQAResult(c.in)
			if got != c.want {
				t.Fatalf("outcome = %v, want %v", got, c.want)
			}
			if why != c.wantWhy {
				t.Fatalf("reason = %q, want %q", why, c.wantWhy)
			}
		})
	}
}

// --- integration harness ------------------------------------------------------

// writeSpecQA writes a task spec carrying BOTH a verify line and an experimental
// `qa:` hint, so the driver's QA gate opts in for the task.
func writeSpecQA(t *testing.T, dir, taskID, verify, qa string) string {
	t.Helper()
	rel := filepath.Join(".pact", "tasks", taskID+".md")
	abs := filepath.Join(dir, rel)
	body := "# " + taskID + "\n\nverify: " + verify + "\nqa: " + qa + "\n"
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return rel
}

// lastQANote returns the most recent QA note event for a task. QA notes reuse the
// `start` event_type (no new event_type) and are distinguished by the `qa_by`
// payload key.
func lastQANote(t *testing.T, dir, task string) (result, by, note string) {
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
		if b, ok := e.Payload["qa_by"].(string); ok {
			r, _ := e.Payload["qa_result"].(string)
			n, _ := e.Payload["qa_note"].(string)
			return r, b, n
		}
	}
	t.Fatalf("no QA note found for task %q", task)
	return "", "", ""
}

// qaRunner drives the pact engine for the QA-gate tests. A worker brief checkpoints;
// a QA brief writes the next verdict from `results` to the task stream (exactly
// where the production runner mirrors the agent's stdout), or returns an error when
// failQA is set (soft-failure path); a fix-round brief re-checkpoints (the owner
// re-run a QA FAIL drives); a reviewer brief captures the briefing and accepts.
type qaRunner struct {
	dir          string
	results      []string // QA_RESULT line emitted on successive QA stints
	failQA       bool     // QA stint returns an error (soft skip)
	workerCalls  int
	qaCalls      int
	fixCalls     int
	reviewCalls  int
	reviewBriefs []string
}

func (r *qaRunner) Run(_ context.Context, lc LaunchContext) error {
	task := taskIDFromBrief(lc.Briefing)
	switch {
	case strings.Contains(lc.Briefing, "pact fix round"):
		r.fixCalls++
		return pact.At(r.dir).As(lc.Seat).Checkpoint(task, "evidence: qa-fixed")
	case strings.Contains(lc.Briefing, "pact QA"):
		r.qaCalls++
		if r.failQA {
			return context.DeadlineExceeded // soft failure: driver must skip leniently
		}
		line := ""
		if idx := r.qaCalls - 1; idx < len(r.results) {
			line = r.results[idx]
		} else if len(r.results) > 0 {
			line = r.results[len(r.results)-1]
		}
		if line != "" {
			if sink, err := OpenStreamSink(lc.streamDir(), task); err == nil {
				_, _ = sink.Write([]byte(line + "\n"))
				_ = sink.Close()
			}
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

// (1) QA PASS → the report path is injected into the reviewer brief and the
// feature ships, with exactly one QA stint.
func TestQAPassProceedsAndInjectsReport(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpecQA(t, dir, "t1", "go test ./...", "log in and see the dashboard")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := &qaRunner{dir: dir, results: []string{"QA_RESULT: PASS: dashboard loads"}}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped", got)
	}
	if runner.qaCalls != 1 {
		t.Fatalf("QA stints = %d, want exactly 1", runner.qaCalls)
	}
	if runner.fixCalls != 0 {
		t.Fatalf("fix rounds = %d, want 0 on a QA PASS", runner.fixCalls)
	}
	if len(runner.reviewBriefs) == 0 {
		t.Fatal("reviewer never ran")
	}
	rb := runner.reviewBriefs[0]
	if !strings.Contains(rb, "qa-t1.md") {
		t.Fatalf("reviewer brief missing QA report path:\n%s", rb)
	}
	if !strings.Contains(rb, "dashboard loads") {
		t.Fatalf("reviewer brief missing QA verdict sentence:\n%s", rb)
	}
	if r, _, _ := lastQANote(t, dir, "t1"); r != "pass" {
		t.Fatalf("QA note result = %q, want pass", r)
	}
}

// (2) QA FAIL → the driver re-runs the owner (a shared fix round) and, once QA
// passes, proceeds to review and ships. Proves FAIL feeds the WS-F fix loop.
func TestQAFailTriggersFixRoundThenProceeds(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpecQA(t, dir, "t1", "go test ./...", "submit the form")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := &qaRunner{dir: dir, results: []string{
		"QA_RESULT: FAIL: submit 500s",
		"QA_RESULT: PASS: submit works",
	}}
	opts := baseOpts(dir, runner, &okExec{}, &recNotify{})
	opts.MaxFixRounds = 2
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped (QA fail→fix→pass→review)", got)
	}
	if runner.fixCalls < 1 {
		t.Fatalf("owner not re-run on a QA FAIL (fixCalls=%d, want >=1)", runner.fixCalls)
	}
	if runner.qaCalls != 2 {
		t.Fatalf("QA stints = %d, want 2 (fail then re-run pass)", runner.qaCalls)
	}
	if runner.reviewCalls < 1 {
		t.Fatalf("reviewer never ran after QA went green (reviewCalls=%d)", runner.reviewCalls)
	}
}

// (2b) QA FAIL shares the fix-round budget with the verify fix loop: with
// MaxFixRounds=1 and a permanently-failing QA verdict, the run escalates on the
// second QA verdict (rounds exhausted) rather than looping forever.
func TestQAFailExhaustsSharedBudgetAndEscalates(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpecQA(t, dir, "t1", "go test ./...", "submit the form")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := &qaRunner{dir: dir, results: []string{"QA_RESULT: FAIL: still broken"}}
	notify := &recNotify{}
	opts := baseOpts(dir, runner, &okExec{}, notify)
	opts.MaxFixRounds = 1
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got == "shipped" {
		t.Fatal("feature shipped despite a QA gate that never passes")
	}
	if runner.reviewCalls != 0 {
		t.Fatalf("reviewer ran on a permanently-failing QA gate (reviewCalls=%d, want 0)", runner.reviewCalls)
	}
	if runner.fixCalls != opts.MaxFixRounds {
		t.Fatalf("QA fix rounds = %d, want exactly MaxFixRounds=%d (shared budget)", runner.fixCalls, opts.MaxFixRounds)
	}
	esc := filepath.Join(dir, ".pact", "orchestrate", "escalation-"+fixedNow()+".md")
	b, err := os.ReadFile(esc)
	if err != nil {
		t.Fatalf("escalation file missing after exhausted QA fix rounds: %v", err)
	}
	if !strings.Contains(string(b), "QA gate FAIL") {
		t.Fatalf("escalation reason should name the QA gate:\n%s", b)
	}
}

// (3) missing QA marker → lenient: the driver records a note and proceeds to
// review (an experimental feature must never hard-block).
func TestQAMissingMarkerProceedsLenient(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpecQA(t, dir, "t1", "go test ./...", "poke the thing")
	assign(t, dir, "t1", "f", "feat/x", s1)

	// QA stint runs but emits no QA_RESULT line (empty results → nothing written).
	runner := &qaRunner{dir: dir, results: []string{"ran some stuff, forgot the verdict line"}}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped — a missing QA verdict must not block", got)
	}
	if runner.qaCalls != 1 {
		t.Fatalf("QA stints = %d, want 1", runner.qaCalls)
	}
	if runner.fixCalls != 0 {
		t.Fatalf("fix rounds = %d, want 0 (missing marker is lenient, not a FAIL)", runner.fixCalls)
	}
	if runner.reviewCalls != 1 {
		t.Fatalf("reviewer runs = %d, want 1 (flow proceeds despite missing marker)", runner.reviewCalls)
	}
	if r, _, _ := lastQANote(t, dir, "t1"); r != "missing" {
		t.Fatalf("QA note result = %q, want missing", r)
	}
}

// (3b) QA stint failure/timeout → lenient: skipped, flow proceeds and ships.
func TestQAStintFailureProceedsLenient(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpecQA(t, dir, "t1", "go test ./...", "run it")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := &qaRunner{dir: dir, failQA: true}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped — a QA stint failure must not block", got)
	}
	if runner.qaCalls != 1 {
		t.Fatalf("QA stints = %d, want 1 (attempted once, then skipped)", runner.qaCalls)
	}
	if runner.reviewCalls != 1 {
		t.Fatalf("reviewer runs = %d, want 1 (flow proceeds despite QA failure)", runner.reviewCalls)
	}
}

// (4) default (no `qa:` line) → zero QA stints and a reviewer brief byte-identical
// to the no-QA build (behavior unchanged).
func TestQANoLineZeroStintsByteIdentical(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...") // no qa: line
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := &qaRunner{dir: dir}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped", got)
	}
	if runner.qaCalls != 0 {
		t.Fatalf("QA stints = %d, want 0 (no qa: line → feature off)", runner.qaCalls)
	}
	if runner.fixCalls != 0 {
		t.Fatalf("fix rounds = %d, want 0", runner.fixCalls)
	}
	if len(runner.reviewBriefs) == 0 {
		t.Fatal("reviewer never ran")
	}
	got := runner.reviewBriefs[0]
	if strings.Contains(got, "评审前置提示") || strings.Contains(got, "qa-t1.md") {
		t.Fatalf("no-qa reviewer brief leaked a pre-review/QA section:\n%s", got)
	}
	seat := projSeat("orch")
	_, task, ok := find(mustState(t, dir), "f", "t1")
	if !ok {
		t.Fatal("task t1 not found in state")
	}
	if want := reviewerBrief(dir, seat, task, ""); got != want {
		t.Fatalf("no-qa reviewer brief not byte-identical.\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}
