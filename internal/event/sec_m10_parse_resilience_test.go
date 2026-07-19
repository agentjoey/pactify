package event

import (
	"strings"
	"testing"
)

// Security/availability regression — review finding M10 (medium).
//
// parse() uses a bufio.Scanner capped at 1 MiB and returns nil,err on the first
// unparseable line. So a single >1 MiB event line (e.g. a worker pasting a huge
// `checkpoint --evidence` blob) trips bufio.ErrTooLong — the scanner then STOPS,
// and every ReadAll/ParseAll (all verbs, status, dashboard, validate, snapshot
// fallback) fails, making the whole project ledger unreadable with no recovery.
// A single malformed line does the same. The ledger read must be resilient: skip
// a bad line (with a warning/count) and return the rest, never brick the project.
//
// RED until parse() skips over-long / unparseable lines instead of erroring the
// entire read.

const (
	m10Valid1 = `{"event_id":"e1","ts":"2026-01-01T00:00:00Z","agent_id":"claude","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{}}`
	m10Valid2 = `{"event_id":"e2","ts":"2026-01-01T00:00:01Z","agent_id":"w","role":"worker","event_type":"join","task_id":"","feature":"","payload":{}}`
)

func TestSEC_M10_ParseSkipsOversizedLine(t *testing.T) {
	oversized := strings.Repeat("x", 2<<20) // 2 MiB, exceeds the 1 MiB scanner cap
	data := []byte(m10Valid1 + "\n" + oversized + "\n" + m10Valid2 + "\n")

	evs, err := ParseAll(data)
	if err != nil {
		t.Fatalf("M10: one oversized line bricked the entire ledger read: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("M10: expected the 2 valid events (oversized line skipped), got %d", len(evs))
	}
}

func TestSEC_M10_ParseSkipsMalformedLine(t *testing.T) {
	data := []byte(m10Valid1 + "\n" + `{not valid json` + "\n" + m10Valid2 + "\n")

	evs, err := ParseAll(data)
	if err != nil {
		t.Fatalf("M10: a malformed line bricked the entire ledger read: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("M10: expected the 2 valid events (malformed line skipped), got %d", len(evs))
	}
}
