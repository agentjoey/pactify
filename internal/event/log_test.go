package event

import (
	"path/filepath"
	"testing"
)

func TestAppendAddsEventIDAndTSAndReadAllRoundtrips(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log.jsonl")
	if err := Append(log, Event{AgentID: "claude-opus", Role: "orchestrator", EventType: "join", Payload: map[string]any{"roles": []any{"worker"}}}); err != nil {
		t.Fatal(err)
	}
	evs, err := ReadAll(log)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("len=%d", len(evs))
	}
	if evs[0].EventID == "" || evs[0].TS == "" {
		t.Fatalf("missing event_id/ts: %+v", evs[0])
	}
	if evs[0].EventType != "join" || evs[0].AgentID != "claude-opus" {
		t.Fatalf("bad event: %+v", evs[0])
	}
}

func TestReadAllIgnoresUnknownTopLevelFields(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log.jsonl")
	line := `{"event_id":"x","ts":"2026-01-01T00:00:00Z","agent_id":"a","role":"worker","event_type":"nudge","task_id":"","feature":"","payload":{},"future":42}` + "\n"
	if err := writeFile(log, line); err != nil {
		t.Fatal(err)
	}
	evs, err := ReadAll(log)
	if err != nil {
		t.Fatalf("ReadAll must ignore unknown fields, got %v", err)
	}
	if len(evs) != 1 || evs[0].EventType != "nudge" {
		t.Fatalf("got %+v", evs)
	}
}

func TestReadAllMissingFileIsEmpty(t *testing.T) {
	evs, err := ReadAll(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil || evs != nil {
		t.Fatalf("want nil,nil got %v,%v", evs, err)
	}
}
