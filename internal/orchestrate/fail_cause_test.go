package orchestrate

import (
	"context"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// errRunner fails every stint with a fixed error (e.g. the ctx deadline error
// a timed-out agent run surfaces).
type errRunner struct{ err error }

func (r errRunner) Run(context.Context, LaunchContext) error { return r.err }

// 2026-07-19 Phase C rerun F2-a: the escalation said a bare "failure limit
// exceeded" although tripped() appends h.LastFail when present — the chain was
// broken at the assignment sites: the reviewer soft-fail path never set
// LastFail at all, and the owner path recorded a canned string without the
// actual error, so a timeout was unattributable. Both paths must record a
// cause that names the timeout.

func failHistory() History {
	return History{Rework: map[string]int{}, Fails: map[string]int{}, LastFail: map[string]string{}}
}

func TestRunOwnerRecordsTimeoutCause(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat-f", spec)

	opts := baseOpts(dir, errRunner{context.DeadlineExceeded}, &okExec{}, &recNotify{})
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	h := failHistory()
	if err := opts.runOwner(context.Background(), st, &h, Action{Kind: ActRunOwner, Feature: "f", Task: "t1", Seat: "w"}); err != nil {
		t.Fatalf("runOwner: %v", err)
	}
	if h.Fails["t1"] != 1 {
		t.Fatalf("owner soft-fail must count, got %d", h.Fails["t1"])
	}
	if c := h.LastFail["t1"]; !strings.Contains(c, "run timeout (--run-timeout) exceeded") {
		t.Fatalf("owner LastFail must carry the mapped timeout attribution, got %q", c)
	}
}

func TestRunReviewerRecordsTimeoutCause(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat-f", spec)
	// Drive t1 to awaiting_review so the reviewer path is reachable.
	w := pact.At(dir).As("w")
	if err := w.Join("w", "worker"); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, dir, "t1-impl-marker", "true") // any tree change so checkpoint has work
	if err := w.Checkpoint("t1", "done"); err != nil {
		t.Fatal(err)
	}

	opts := baseOpts(dir, errRunner{context.DeadlineExceeded}, &okExec{}, &recNotify{})
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	h := failHistory()
	if err := opts.runReviewer(context.Background(), st, &h, Action{Kind: ActRunReviewer, Feature: "f", Task: "t1", Seat: "orch"}, ""); err != nil {
		t.Fatalf("runReviewer: %v", err)
	}
	if h.Fails["t1"] != 1 {
		t.Fatalf("reviewer soft-fail must count, got %d", h.Fails["t1"])
	}
	if c := h.LastFail["t1"]; !strings.Contains(c, "run timeout (--run-timeout) exceeded") {
		t.Fatalf("reviewer LastFail must carry the mapped timeout attribution, got %q", c)
	}
}
