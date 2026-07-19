package orchestrate

import (
	"context"
	"testing"
	"time"
)

// Concurrency regression — review finding M17.
//
// serve's stale-run guard retires a run whose status.json is >10min old. But an
// agent turn can block up to RunTimeout (30min) without the loop writing a new
// status, so a LIVE run looked stale; a concurrent serve restart (which loses the
// in-memory running-marker) then let a SECOND driver spawn on the same feature. A
// heartbeat keeps a live run's status.json fresh so the guard never misjudges it.

func TestSEC_M17_RestampKeepsLiveRunFresh(t *testing.T) {
	dir := t.TempDir()
	stale := time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339)

	// A live (non-terminal) run whose last status is stale — a long silent stint.
	if err := writeStatus(dir, Status{Feature: "f", Phase: "running", UpdatedAt: stale}); err != nil {
		t.Fatal(err)
	}
	restampStatus(dir, nil) // nil → real RFC3339 now

	s := readStatus(t, dir)
	ts, err := time.Parse(time.RFC3339, s.UpdatedAt)
	if err != nil {
		t.Fatalf("updated_at not RFC3339 after restamp: %q", s.UpdatedAt)
	}
	if time.Since(ts) > 2*time.Minute {
		t.Errorf("restamp did not refresh the stale timestamp: %s", s.UpdatedAt)
	}
	if s.Phase != "running" || s.Feature != "f" {
		t.Errorf("restamp altered non-timestamp fields: %+v", s)
	}

	// A TERMINAL run must NOT be refreshed — serve must be able to retire it.
	doneTS := time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339)
	if err := writeStatus(dir, Status{Feature: "f", Done: true, UpdatedAt: doneTS}); err != nil {
		t.Fatal(err)
	}
	restampStatus(dir, nil)
	if s2 := readStatus(t, dir); s2.UpdatedAt != doneTS {
		t.Errorf("restamp refreshed a DONE run (%q → %q); it must stay retired", doneTS, s2.UpdatedAt)
	}
}

func TestSEC_M17_HeartbeatRefreshesStaleStatus(t *testing.T) {
	dir := t.TempDir()
	stale := time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339)
	if err := writeStatus(dir, Status{Feature: "f", Phase: "running", UpdatedAt: stale}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go heartbeatStatus(ctx, dir, 20*time.Millisecond, nil)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s := readStatus(t, dir)
		if ts, err := time.Parse(time.RFC3339, s.UpdatedAt); err == nil && time.Since(ts) < time.Minute {
			return // the heartbeat refreshed the stale status — a live run stays "alive"
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("heartbeat did not refresh the stale status within 2s")
}
