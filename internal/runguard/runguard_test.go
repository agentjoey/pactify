package runguard

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func serialStatus(t *testing.T, dir, body string) {
	t.Helper()
	write(t, filepath.Join(dir, ".pact", "orchestrate", "status.json"), body)
}

func parallelStatus(t *testing.T, dir, feature, body string) {
	t.Helper()
	write(t, filepath.Join(dir, ".pact", "orchestrate", "parallel", feature+".json"), body)
}

func at(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339) }

// A live run driving another task is exactly the UI-GATE incident: a manual
// checkpoint would sweep that run's in-flight files into this task's commit.
func TestBlocksCheckpointWhenLiveRunDrivesAnotherTask(t *testing.T) {
	dir := t.TempDir()
	serialStatus(t, dir, `{"feature":"m4-s11","task":"m4-s11-parse","seat":"opencode-worker","done":false,"escalated":false,"updated_at":"`+at(0)+`"}`)

	blocking := BlocksCheckpoint(dir, "m3-s10-brief")

	if len(blocking) != 1 {
		t.Fatalf("want 1 blocking run, got %d: %+v", len(blocking), blocking)
	}
	if blocking[0].Task != "m4-s11-parse" {
		t.Errorf("Task = %q, want m4-s11-parse", blocking[0].Task)
	}
	if blocking[0].Seat != "opencode-worker" {
		t.Errorf("Seat = %q, want opencode-worker", blocking[0].Seat)
	}
}

// The worker's own checkpoint is the protocol's normal handoff (brief.go tells
// it to run `pactify checkpoint <task>`), so the run's current task must pass.
func TestAllowsCheckpointOfTheTaskTheRunIsDriving(t *testing.T) {
	dir := t.TempDir()
	serialStatus(t, dir, `{"feature":"m4-s11","task":"m4-s11-parse","seat":"opencode-worker","done":false,"escalated":false,"updated_at":"`+at(0)+`"}`)

	if blocking := BlocksCheckpoint(dir, "m4-s11-parse"); len(blocking) != 0 {
		t.Fatalf("worker checkpoint of its own task must pass, blocked by %+v", blocking)
	}
}

// A crashed driver leaves done=false forever; an old stamp must not wedge the
// repo shut (same rule serve already uses to decide a run is dead).
func TestStaleRunDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	serialStatus(t, dir, `{"task":"m4-s11-parse","done":false,"escalated":false,"updated_at":"`+at(-20*time.Minute)+`"}`)

	if blocking := BlocksCheckpoint(dir, "m3-s10-brief"); len(blocking) != 0 {
		t.Fatalf("stale run must not block, got %+v", blocking)
	}
}

func TestFinishedRunDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	serialStatus(t, dir, `{"task":"m4-s11-parse","done":true,"escalated":false,"updated_at":"`+at(0)+`"}`)

	if blocking := BlocksCheckpoint(dir, "m3-s10-brief"); len(blocking) != 0 {
		t.Fatalf("finished run must not block, got %+v", blocking)
	}
}

func TestEscalatedRunDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	serialStatus(t, dir, `{"task":"m4-s11-parse","done":false,"escalated":true,"updated_at":"`+at(0)+`"}`)

	if blocking := BlocksCheckpoint(dir, "m3-s10-brief"); len(blocking) != 0 {
		t.Fatalf("escalated run is parked for a human, must not block, got %+v", blocking)
	}
}

// A parallel run writes only parallel/<feature>.json — never status.json — so a
// guard that reads the serial file alone misses concurrent runs entirely.
func TestParallelFeatureStatusBlocks(t *testing.T) {
	dir := t.TempDir()
	parallelStatus(t, dir, "m4-s11", `{"feature":"m4-s11","task":"m4-s11-parse","seat":"kimi-worker","done":false,"escalated":false,"updated_at":"`+at(0)+`"}`)

	blocking := BlocksCheckpoint(dir, "m3-s10-brief")

	if len(blocking) != 1 {
		t.Fatalf("want 1 blocking run from parallel status, got %d: %+v", len(blocking), blocking)
	}
	if blocking[0].Feature != "m4-s11" {
		t.Errorf("Feature = %q, want m4-s11", blocking[0].Feature)
	}
}

func TestParallelRunAllowsItsOwnTask(t *testing.T) {
	dir := t.TempDir()
	parallelStatus(t, dir, "m4-s11", `{"feature":"m4-s11","task":"m4-s11-parse","done":false,"escalated":false,"updated_at":"`+at(0)+`"}`)

	if blocking := BlocksCheckpoint(dir, "m4-s11-parse"); len(blocking) != 0 {
		t.Fatalf("worker checkpoint of its own task must pass, blocked by %+v", blocking)
	}
}

// Several features in flight: only the ones not driving this task block, and
// every one of them is reported so the message can name them all.
func TestReportsEveryBlockingRun(t *testing.T) {
	dir := t.TempDir()
	parallelStatus(t, dir, "m4-s11", `{"feature":"m4-s11","task":"m4-s11-parse","done":false,"escalated":false,"updated_at":"`+at(0)+`"}`)
	parallelStatus(t, dir, "m5-s01", `{"feature":"m5-s01","task":"m5-s01-wire","done":false,"escalated":false,"updated_at":"`+at(0)+`"}`)

	if blocking := BlocksCheckpoint(dir, "m3-s10-brief"); len(blocking) != 2 {
		t.Fatalf("want both live features reported, got %d: %+v", len(blocking), blocking)
	}
}

func TestNoRuntimeFilesAllow(t *testing.T) {
	if blocking := BlocksCheckpoint(t.TempDir(), "m3-s10-brief"); len(blocking) != 0 {
		t.Fatalf("clean repo must not block, got %+v", blocking)
	}
}

// A half-written or corrupt status must not wedge checkpoints shut: unparseable
// means "no evidence of a live run", the same call serve makes.
func TestUnparseableStatusDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	serialStatus(t, dir, `{"task":`)

	if blocking := BlocksCheckpoint(dir, "m3-s10-brief"); len(blocking) != 0 {
		t.Fatalf("corrupt status must not block, got %+v", blocking)
	}
}
