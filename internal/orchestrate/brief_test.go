package orchestrate

import (
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/projection"
)

func mustContain(t *testing.T, body, sub, label string) {
	t.Helper()
	if !strings.Contains(body, sub) {
		t.Errorf("%s: brief missing %q\n---\n%s", label, sub, body)
	}
}

func TestWorkerBrief(t *testing.T) {
	seat := projection.Seat{ID: "alice", Roles: []string{"worker", "backend"}}
	task := projection.Task{ID: "t-42", Owner: "alice", Status: "assigned", Spec: "docs/specs/t-42.md"}

	body := workerBrief("", seat, task, "", false)

	for _, c := range []struct{ sub, label string }{
		{"alice", "seat id"},
		{"worker", "roles"},
		{"backend", "roles"},
		{"pactify join", "join verb"},
		{"t-42", "task id"},
		{"docs/specs/t-42.md", "spec path"},
		{"pactify checkpoint", "checkpoint verb"},
	} {
		mustContain(t, body, c.sub, c.label)
	}
	// Must convey that the worker cannot self-accept.
	if !strings.Contains(body, "accepted") {
		t.Errorf("worker brief should mention not self-marking accepted:\n%s", body)
	}
	// No rework reason given -> the brief should not invent one.
	if strings.Contains(body, "返工原因") || strings.Contains(body, "上次评审") {
		t.Errorf("worker brief leaked rework section with empty changesReason:\n%s", body)
	}
}

func TestWorkerBriefWithChangesReason(t *testing.T) {
	seat := projection.Seat{ID: "alice", Roles: []string{"worker"}}
	task := projection.Task{ID: "t-7", Owner: "alice", Status: "changes_requested", Spec: "spec/t-7.md"}
	reason := "boundary check missing on empty slice input"

	body := workerBrief("", seat, task, reason, false)

	mustContain(t, body, reason, "changes reason verbatim")
	mustContain(t, body, "t-7", "task id")
	mustContain(t, body, "pactify checkpoint", "checkpoint verb")
}

func TestReviewerBrief(t *testing.T) {
	seat := projection.Seat{ID: "bob", Roles: []string{"reviewer"}}
	task := projection.Task{ID: "t-99", Owner: "alice", Reviewer: "bob", Status: "awaiting_review", Spec: "docs/specs/t-99.md"}

	body := reviewerBrief("", seat, task, "")

	for _, c := range []struct{ sub, label string }{
		{"bob", "seat id"},
		{"t-99", "task id"},
		{"docs/specs/t-99.md", "spec path"},
		{"git diff", "diff hint"},
		{"git log", "log hint"},
		{"pactify accept t-99", "accept verb with id"},
		{"pactify changes t-99", "changes verb with id"},
		{"--reason", "changes reason flag"},
	} {
		mustContain(t, body, c.sub, c.label)
	}
	// Must instruct running the spec's acceptance commands.
	if !strings.Contains(body, "验收") {
		t.Errorf("reviewer brief should instruct running acceptance commands:\n%s", body)
	}
	// Must tell the reviewer not to change the implementation themselves.
	if !strings.Contains(body, "不要自己改") && !strings.Contains(body, "不自己改") {
		t.Errorf("reviewer brief should forbid editing the implementation:\n%s", body)
	}
}

// TestWorkerBrief_MultilineReasonStaysQuoted (review #4): a multi-line changes
// reason must stay entirely inside the markdown blockquote.
func TestWorkerBrief_MultilineReasonStaysQuoted(t *testing.T) {
	seat := projection.Seat{ID: "w", Roles: []string{"worker"}}
	task := projection.Task{ID: "t1", Spec: ".pact/tasks/t1.md"}
	out := workerBrief("", seat, task, "line one\nline two", false)
	if !strings.Contains(out, "> line one\n> line two") {
		t.Fatalf("multi-line reason not fully quoted:\n%s", out)
	}
}

func TestWorkerBriefRetryingHasContinuation(t *testing.T) {
	seat := projection.Seat{ID: "w1", Roles: []string{"worker"}}
	task := projection.Task{ID: "t1", Spec: ".pact/tasks/t1.md"}
	out := workerBrief("", seat, task, "", true)
	if !strings.Contains(out, "重试棒") {
		t.Error("retrying brief should flag it is a retry")
	}
	if !strings.Contains(out, "git status") || !strings.Contains(out, "半成品") {
		t.Error("retrying brief should tell the worker to inspect half-done work")
	}
	// A non-retry brief must NOT carry the continuation section.
	if strings.Contains(workerBrief("", seat, task, "", false), "重试棒") {
		t.Error("non-retry brief should not have the continuation section")
	}
}
