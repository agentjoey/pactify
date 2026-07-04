package projection

import (
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/event"
)

// qInit seeds a roster with an owner and three reviewer seats for quorum tests.
func qInit() event.Event {
	return event.Event{EventType: "init", AgentID: "orch", Payload: map[string]any{
		"project": "p", "protocol_version": 1.0,
		"seats": []any{
			map[string]any{"id": "orch", "roles": []any{"orchestrator"}},
			map[string]any{"id": "worker", "roles": []any{"worker"}},
			map[string]any{"id": "a", "roles": []any{"reviewer"}},
			map[string]any{"id": "b", "roles": []any{"reviewer"}},
			map[string]any{"id": "c", "roles": []any{"reviewer"}},
		},
	}}
}

func qAssign(reviewers []any, quorum int) event.Event {
	return event.Event{EventType: "assign", TaskID: "T1", Feature: "F", AgentID: "orch",
		Payload: map[string]any{"owner": "worker", "reviewers": reviewers, "quorum": quorum, "branch": "feat/x", "spec": "s"}}
}

func qCheckpoint() event.Event {
	return event.Event{EventType: "checkpoint", TaskID: "T1", Feature: "F", AgentID: "worker", Payload: map[string]any{"evidence": "ok"}}
}

func qAccept(seat string) event.Event {
	return event.Event{EventType: "accept", TaskID: "T1", Feature: "F", AgentID: seat, Payload: map[string]any{}}
}

func qChanges(seat string) event.Event {
	return event.Event{EventType: "changes_requested", TaskID: "T1", Feature: "F", AgentID: seat, Payload: map[string]any{"reason": "fix"}}
}

func onlyTask(t *testing.T, st State) Task {
	t.Helper()
	if len(st.Features) != 1 || len(st.Features[0].Tasks) != 1 {
		t.Fatalf("want one feature/one task, got %+v", st.Features)
	}
	return st.Features[0].Tasks[0]
}

// 2 of 3 quorum: one accept stays awaiting_review; the second distinct accept
// reaches quorum and flips to accepted.
func TestQuorumReachedTwoOfThree(t *testing.T) {
	base := []event.Event{qInit(), qAssign([]any{"a", "b", "c"}, 2), qCheckpoint()}

	afterOne := Project(append(append([]event.Event{}, base...), qAccept("a")))
	tk := onlyTask(t, afterOne)
	if tk.Status != "awaiting_review" {
		t.Fatalf("after 1 accept want awaiting_review, got %q", tk.Status)
	}
	if len(tk.Accepts) != 1 || tk.Accepts[0] != "a" {
		t.Fatalf("accepts tally want [a], got %v", tk.Accepts)
	}
	if tk.Reviewer != "a" || tk.Quorum != 2 || len(tk.Reviewers) != 3 {
		t.Fatalf("quorum fields wrong: reviewer=%q quorum=%d reviewers=%v", tk.Reviewer, tk.Quorum, tk.Reviewers)
	}

	afterTwo := Project(append(append([]event.Event{}, base...), qAccept("a"), qAccept("b")))
	tk = onlyTask(t, afterTwo)
	if tk.Status != "accepted" {
		t.Fatalf("after 2 distinct accepts want accepted, got %q", tk.Status)
	}
}

// The same reviewer accepting twice is ONE distinct vote: it must not reach a
// quorum of 2 on its own.
func TestQuorumDistinctReviewersOnly(t *testing.T) {
	evs := []event.Event{qInit(), qAssign([]any{"a", "b", "c"}, 2), qCheckpoint(), qAccept("a"), qAccept("a")}
	tk := onlyTask(t, Project(evs))
	if tk.Status != "awaiting_review" {
		t.Fatalf("double accept by same reviewer must not meet quorum, got %q", tk.Status)
	}
	if len(tk.Accepts) != 1 {
		t.Fatalf("accepts tally want 1 distinct, got %v", tk.Accepts)
	}
}

// A changes_requested resets the round: after a re-checkpoint all reviewers must
// vote afresh (the pre-changes accept no longer counts).
func TestQuorumChangesResetsTally(t *testing.T) {
	evs := []event.Event{
		qInit(), qAssign([]any{"a", "b"}, 2), qCheckpoint(),
		qAccept("a"),  // 1/2
		qChanges("b"), // reset → changes_requested, tally cleared
	}
	tk := onlyTask(t, Project(evs))
	if tk.Status != "changes_requested" {
		t.Fatalf("want changes_requested, got %q", tk.Status)
	}
	if len(tk.Accepts) != 0 {
		t.Fatalf("changes must clear the tally, got %v", tk.Accepts)
	}

	// Re-checkpoint and re-vote: a alone (its pre-changes accept) must NOT suffice.
	reworked := append(append([]event.Event{}, evs...), qCheckpoint(), qAccept("a"))
	tk = onlyTask(t, Project(reworked))
	if tk.Status != "awaiting_review" {
		t.Fatalf("after re-checkpoint one accept must not meet quorum, got %q (accepts %v)", tk.Status, tk.Accepts)
	}

	// The second distinct reviewer completes the fresh round.
	done := append(append([]event.Event{}, reworked...), qAccept("b"))
	if got := onlyTask(t, Project(done)).Status; got != "accepted" {
		t.Fatalf("fresh round of 2 distinct accepts want accepted, got %q", got)
	}
}

// A re-checkpoint (without a changes verdict) also opens a fresh round.
func TestQuorumRecheckpointReCounts(t *testing.T) {
	evs := []event.Event{
		qInit(), qAssign([]any{"a", "b"}, 2), qCheckpoint(),
		qAccept("a"),  // 1/2
		qCheckpoint(), // new round, tally cleared
	}
	tk := onlyTask(t, Project(evs))
	if tk.Status != "awaiting_review" || len(tk.Accepts) != 0 {
		t.Fatalf("re-checkpoint must reopen a clean round, got status %q accepts %v", tk.Status, tk.Accepts)
	}
}

// GOLDEN: a legacy single-reviewer assign folds and renders byte-identically to the
// pre-quorum behavior — no reviewers/quorum/accepts fields, and one accept accepts.
func TestSingleReviewerGoldenUnchanged(t *testing.T) {
	assign := event.Event{EventType: "assign", TaskID: "T1", Feature: "F", AgentID: "orch",
		Payload: map[string]any{"owner": "worker", "reviewer": "a", "branch": "feat/x", "spec": "s"}}

	// Fold: legacy fields stay zero-valued; one accept → accepted.
	afterCheckpoint := Project([]event.Event{qInit(), assign, qCheckpoint()})
	tk := onlyTask(t, afterCheckpoint)
	if tk.Reviewer != "a" || tk.Reviewers != nil || tk.Quorum != 0 || tk.Accepts != nil {
		t.Fatalf("legacy task must carry no quorum state: %+v", tk)
	}
	if got := onlyTask(t, Project([]event.Event{qInit(), assign, qCheckpoint(), qAccept("a")})).Status; got != "accepted" {
		t.Fatalf("legacy single accept want accepted, got %q", got)
	}

	// Render: STATE.yml for the awaiting_review legacy task has NO quorum lines.
	out := Render(afterCheckpoint)
	if strings.Contains(out, "reviewers:") || strings.Contains(out, "quorum:") || strings.Contains(out, "accepts:") {
		t.Fatalf("legacy render must not emit quorum lines:\n%s", out)
	}
	// The exact task block the bash reference would render (byte-for-byte).
	wantBlock := "      - id: T1\n" +
		"        owner: worker\n" +
		"        status: awaiting_review\n" +
		"        reviewer: a\n" +
		"        spec: s\n" +
		"        evidence: ok\n"
	if !strings.Contains(out, wantBlock) {
		t.Fatalf("legacy task block drifted:\n--- got ---\n%s\n--- want block ---\n%s", out, wantBlock)
	}
}

// A quorum task DOES render its quorum lines (the additive, opt-in surface).
func TestQuorumRenderLines(t *testing.T) {
	st := Project([]event.Event{qInit(), qAssign([]any{"a", "b", "c"}, 2), qCheckpoint(), qAccept("a")})
	out := Render(st)
	for _, want := range []string{"reviewers: [a, b, c]", "quorum: 2", "accepts: [a]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("quorum render missing %q:\n%s", want, out)
		}
	}
}
