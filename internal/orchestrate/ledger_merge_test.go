package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mergeEventLines unions two append-only JSONL logs by event_id, base first then
// extra's new lines, dropping duplicates.
func TestMergeEventLines(t *testing.T) {
	base := []byte(`{"event_id":"a"}` + "\n" + `{"event_id":"b"}` + "\n")
	extra := []byte(`{"event_id":"a"}` + "\n" + `{"event_id":"c"}` + "\n")
	got := string(mergeEventLines(base, extra))
	want := `{"event_id":"a"}` + "\n" + `{"event_id":"b"}` + "\n" + `{"event_id":"c"}` + "\n"
	if got != want {
		t.Fatalf("mergeEventLines = %q, want %q", got, want)
	}
}

// 回灌 must MERGE the sandbox ledger into main, not overwrite it — a concurrent
// event appended to main between seed and copy-back must survive.
func TestWriteLedgerPreservesConcurrentMainEvents(t *testing.T) {
	dir := newProject(t)
	logPath := filepath.Join(dir, ".pact", "log.jsonl")

	// Snapshot the sandbox's view (= main at seed time), then the sandbox advances.
	snap := readLedger(dir)
	sbxLine := `{"event_id":"sbxC","ts":"2026-06-24T00:00:00Z","agent_id":"orch","role":"orchestrator","event_type":"config_gate","payload":{"gate":"x"}}` + "\n"
	snap["log.jsonl"] = append(append([]byte{}, snap["log.jsonl"]...), []byte(sbxLine)...)

	// Meanwhile main gains a concurrent event the sandbox never saw.
	concLine := `{"event_id":"concD","ts":"2026-06-24T00:00:01Z","agent_id":"orch","role":"orchestrator","event_type":"config_gate","payload":{"gate":"y"}}` + "\n"
	f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(concLine)
	f.Close()

	writeLedger(dir, snap)

	final, _ := os.ReadFile(logPath)
	if !strings.Contains(string(final), "sbxC") {
		t.Fatal("sandbox event sbxC lost on copy-back")
	}
	if !strings.Contains(string(final), "concD") {
		t.Fatal("concurrent main event concD clobbered by copy-back (overwrite instead of merge)")
	}
}
