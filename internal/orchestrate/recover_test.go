package orchestrate

import (
	"context"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// After a worker soft-fail, if the task's verify command now passes the work is
// actually done — classifyAndCheckpoint records a checkpoint (as the owner) and
// the task moves to awaiting_review, instead of re-burning the worker.
func TestClassifyAndCheckpoint_VerifyPasses(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "F", "feat-F", spec)

	opts := Options{Dir: dir, Exec: &okExec{}}
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	_, task, ok := find(st, "F", "t1")
	if !ok {
		t.Fatal("task t1 not found")
	}

	if !opts.classifyAndCheckpoint(context.Background(), "t1", task) {
		t.Fatal("verify passing should return true (checkpoint recorded)")
	}
	after, _ := pact.At(dir).StateProjection()
	if _, t1, _ := find(after, "F", "t1"); t1.Status != "awaiting_review" {
		t.Fatalf("status=%q, want awaiting_review", t1.Status)
	}
}

// When the verify command fails, the work is genuinely incomplete: return false
// so the driver keeps retrying the worker (no checkpoint, task stays assigned).
func TestClassifyAndCheckpoint_VerifyFails(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "false")
	assign(t, dir, "t1", "F", "feat-F", spec)

	opts := Options{Dir: dir, Exec: &failExec{}}
	st, _ := pact.At(dir).StateProjection()
	_, task, _ := find(st, "F", "t1")

	if opts.classifyAndCheckpoint(context.Background(), "t1", task) {
		t.Fatal("verify failing should return false")
	}
	after, _ := pact.At(dir).StateProjection()
	if _, t1, _ := find(after, "F", "t1"); t1.Status != "assigned" {
		t.Fatalf("status=%q, want assigned (no checkpoint on fail)", t1.Status)
	}
}
