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
	assign(t, dir, "t1", "f", "feat-f", spec)

	opts := Options{Dir: dir, Exec: &okExec{}}
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	_, task, ok := find(st, "f", "t1")
	if !ok {
		t.Fatal("task t1 not found")
	}

	if !opts.classifyAndCheckpoint(context.Background(), "t1", task, true) {
		t.Fatal("verify passing should return true (checkpoint recorded)")
	}
	after, _ := pact.At(dir).StateProjection()
	if _, t1, _ := find(after, "f", "t1"); t1.Status != "awaiting_review" {
		t.Fatalf("status=%q, want awaiting_review", t1.Status)
	}
}

// F2-b: a stint that delivered NOTHING (launch-window tree fingerprint
// unchanged) is never rescued, no matter how green its verify is — a weak gate
// must not promote phantom work.
func TestClassifyAndCheckpoint_NoDeliveryNoRescue(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat-f", spec)

	opts := Options{Dir: dir, Exec: &okExec{}}
	st, _ := pact.At(dir).StateProjection()
	_, task, _ := find(st, "f", "t1")

	if opts.classifyAndCheckpoint(context.Background(), "t1", task, false) {
		t.Fatal("no delivery must mean no rescue, even with a green verify")
	}
	after, _ := pact.At(dir).StateProjection()
	if _, t1, _ := find(after, "f", "t1"); t1.Status != "assigned" {
		t.Fatalf("status=%q, want assigned (no phantom checkpoint)", t1.Status)
	}
}

// When the verify command fails, the work is genuinely incomplete: return false
// so the driver keeps retrying the worker (no checkpoint, task stays assigned).
func TestClassifyAndCheckpoint_VerifyFails(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "false")
	assign(t, dir, "t1", "f", "feat-f", spec)

	opts := Options{Dir: dir, Exec: &failExec{}}
	st, _ := pact.At(dir).StateProjection()
	_, task, _ := find(st, "f", "t1")

	if opts.classifyAndCheckpoint(context.Background(), "t1", task, true) {
		t.Fatal("verify failing should return false")
	}
	after, _ := pact.At(dir).StateProjection()
	if _, t1, _ := find(after, "f", "t1"); t1.Status != "assigned" {
		t.Fatalf("status=%q, want assigned (no checkpoint on fail)", t1.Status)
	}
}
