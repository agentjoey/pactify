package orchestrate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/projection"
)

func statusPath(dir string) string {
	return filepath.Join(dir, ".pact", "orchestrate", "status.json")
}

// --- unit: writeStatus --------------------------------------------------------

func TestWriteStatusWritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	s := Status{
		Feature:   "f1",
		Task:      "t1",
		Seat:      "w",
		Action:    "run_owner",
		Phase:     "owner working",
		Escalated: false,
		Done:      false,
		Total:     3,
		Accepted:  1,
		Iter:      0,
		UpdatedAt: "2026-06-13T12:00:00Z",
	}
	if err := writeStatus(dir, s); err != nil {
		t.Fatalf("writeStatus: %v", err)
	}

	data, err := os.ReadFile(statusPath(dir))
	if err != nil {
		t.Fatalf("read status.json: %v", err)
	}

	var got Status
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal status.json: %v", err)
	}
	if got.Feature != s.Feature || got.Task != s.Task || got.Action != s.Action {
		t.Fatalf("roundtrip mismatch: got %+v, want %+v", got, s)
	}
	if got.Total != 3 || got.Accepted != 1 {
		t.Fatalf("progress fields: total=%d accepted=%d", got.Total, got.Accepted)
	}
}

func TestWriteStatusNoTempFileLeftBehind(t *testing.T) {
	dir := t.TempDir()
	s := Status{Action: "idle", UpdatedAt: "ts"}
	if err := writeStatus(dir, s); err != nil {
		t.Fatalf("writeStatus: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".pact", "orchestrate", "status.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("temp file status.json.tmp should not exist after atomic write")
	}
	if _, err := os.Stat(statusPath(dir)); err != nil {
		t.Fatalf("final status.json missing: %v", err)
	}
}

func TestWriteStatusCreatesDirIfAbsent(t *testing.T) {
	dir := t.TempDir()
	s := Status{Action: "done", Done: true, UpdatedAt: "ts"}
	if err := writeStatus(dir, s); err != nil {
		t.Fatalf("writeStatus: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".pact", "orchestrate", "status.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("temp file status.json.tmp should not exist after atomic write")
	}
	if _, err := os.Stat(statusPath(dir)); err != nil {
		t.Fatalf("final status.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".pact", "orchestrate")); err != nil {
		t.Fatalf("orchestrate dir missing: %v", err)
	}
}

// --- unit: progress -----------------------------------------------------------

func TestProgressCountsTotalAndAccepted(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{
			{ID: "f1", Tasks: []projection.Task{
				{ID: "t1", Status: "accepted"},
				{ID: "t2", Status: "assigned"},
				{ID: "t3", Status: "awaiting_review"},
			}},
			{ID: "f2", Tasks: []projection.Task{
				{ID: "t4", Status: "accepted"},
				{ID: "t5", Status: "changes_requested"},
			}},
		},
	}
	total, accepted := progress(st)
	if total != 5 {
		t.Fatalf("total=%d, want 5", total)
	}
	if accepted != 2 {
		t.Fatalf("accepted=%d, want 2", accepted)
	}
}

func TestProgressEmptyState(t *testing.T) {
	total, accepted := progress(projection.State{})
	if total != 0 || accepted != 0 {
		t.Fatalf("empty state: total=%d accepted=%d", total, accepted)
	}
}

func TestProgressShippedFeatureCountsTasks(t *testing.T) {
	st := projection.State{
		Features: []projection.Feature{
			{ID: "f1", Status: "shipped", Tasks: []projection.Task{
				{ID: "t1", Status: "accepted"},
				{ID: "t2", Status: "accepted"},
			}},
		},
	}
	total, accepted := progress(st)
	if total != 2 {
		t.Fatalf("total=%d, want 2 (shipped features count too)", total)
	}
	if accepted != 2 {
		t.Fatalf("accepted=%d, want 2", accepted)
	}
}

// --- unit: status JSON tags match contract ------------------------------------

func TestStatusJSONTagsMatchContract(t *testing.T) {
	s := Status{
		Feature:   "f",
		Task:      "t",
		Seat:      "s",
		Action:    "run_owner",
		Phase:     "owner working",
		Escalated: true,
		Reason:    "stuck",
		Done:      false,
		Total:     10,
		Accepted:  5,
		Iter:      3,
		UpdatedAt: "2026-06-13T12:00:00Z",
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}

	// Every field in the contract must be present (except omitempty when empty).
	required := []string{"feature", "task", "seat", "action", "phase", "escalated", "done", "total", "accepted", "iter", "updated_at"}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			t.Fatalf("contract field %q missing from JSON", k)
		}
	}
	// reason is omitempty — present when set, absent when empty.
	if _, ok := m["reason"]; !ok {
		t.Fatal("reason should be present when non-empty")
	}

	// Verify omitempty: empty reason is omitted.
	s2 := Status{Action: "idle", UpdatedAt: "ts"}
	data2, _ := json.Marshal(s2)
	var m2 map[string]interface{}
	json.Unmarshal(data2, &m2)
	if _, ok := m2["reason"]; ok {
		t.Fatal("reason should be omitted when empty")
	}
}

// --- integration: loop writes status after one round --------------------------

func TestLoopWritesStatusAfterRunOwnerRound(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "T1", "go test ./...")
	assign(t, dir, "T1", "F", "feat/x", s1)

	runner := newFakeRunner(t, dir)
	exec := &okExec{}
	notify := &recNotify{}
	opts := baseOpts(dir, runner, exec, notify)
	opts.Th.MaxIters = 0 // allow unlimited

	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	sp := statusPath(dir)
	data, err := os.ReadFile(sp)
	if err != nil {
		t.Fatalf("status.json missing after loop run: %v", err)
	}
	var s Status
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal status.json: %v\n%s", err, string(data))
	}
	if s.Action != "done" {
		t.Fatalf("final action = %q, want done", s.Action)
	}
	if !s.Done {
		t.Fatal("Done should be true after loop completes")
	}
	if s.Total == 0 {
		t.Fatal("Total should be > 0")
	}
}

func TestLoopDryRunDoesNotWriteStatus(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "T1", "go test ./...")
	assign(t, dir, "T1", "F", "feat/x", s1)

	runner := newFakeRunner(t, dir)
	exec := &okExec{}
	notify := &recNotify{}
	opts := baseOpts(dir, runner, exec, notify)
	opts.DryRun = true
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	sp := statusPath(dir)
	if _, err := os.Stat(sp); !os.IsNotExist(err) {
		t.Fatal("status.json should not exist after DryRun")
	}
}

func TestLoopWritesEscalatedStatusOnReworkLimit(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "T1", "go test ./...")
	assign(t, dir, "T1", "F", "feat/x", s1)

	runner := newFakeRunner(t, dir)
	runner.alwaysChanges = true
	opts := baseOpts(dir, runner, &okExec{}, &recNotify{})
	opts.Th.MaxRework = 2
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	sp := statusPath(dir)
	data, err := os.ReadFile(sp)
	if err != nil {
		t.Fatalf("status.json missing after escalation: %v", err)
	}
	var s Status
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal status.json: %v\n%s", err, string(data))
	}
	if !s.Escalated {
		t.Fatal("Escalated should be true after escalation")
	}
	if s.Action != "stuck" {
		t.Fatalf("action = %q, want stuck", s.Action)
	}
	if s.Reason == "" {
		t.Fatal("Reason should be non-empty on escalation")
	}
}

func TestLoopWritesEscalatedStatusOnGateFailure(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "T1", "go test ./...")
	assign(t, dir, "T1", "F", "feat/x", s1)

	runner := newFakeRunner(t, dir)
	exec := &failExec{}
	opts := baseOpts(dir, runner, exec, &recNotify{})
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	sp := statusPath(dir)
	data, err := os.ReadFile(sp)
	if err != nil {
		t.Fatalf("status.json missing after gate escalation: %v", err)
	}
	var s Status
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal status.json: %v\n%s", err, string(data))
	}
	if !s.Escalated {
		t.Fatal("Escalated should be true after gate failure")
	}
	if s.Reason == "" {
		t.Fatal("Reason should be non-empty on gate failure")
	}
}

func TestLoopWritesEscalatedStatusOnFailLimit(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f1", "feat-f1", spec)

	run := &crashRunner{dir: dir, crashes: 99}
	opts := baseOpts(dir, run, &okExec{}, &recNotify{})
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	sp := statusPath(dir)
	data, err := os.ReadFile(sp)
	if err != nil {
		t.Fatalf("status.json missing after fail escalation: %v", err)
	}
	var s Status
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal status.json: %v\n%s", err, string(data))
	}
	if !s.Escalated {
		t.Fatal("Escalated should be true after fail limit")
	}
}

func TestLoopWritesStatusPerIteration(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "T1", "go test ./...")
	assign(t, dir, "T1", "F", "feat/x", s1)

	runner := newFakeRunner(t, dir)
	runner.changesBeforeAccept = 1 // one rework round
	exec := &okExec{}
	notify := &recNotify{}
	opts := baseOpts(dir, runner, exec, notify)

	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	sp := statusPath(dir)
	data, err := os.ReadFile(sp)
	if err != nil {
		t.Fatalf("status.json missing after loop: %v", err)
	}
	var s Status
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal status.json: %v", err)
	}
	// Final write should be "done" after feature shipped.
	if s.Action != "done" {
		t.Fatalf("final action = %q, want done", s.Action)
	}
	if !s.Done {
		t.Fatal("Done should be true after loop completes")
	}
	if s.Total < 1 || s.Accepted < 1 {
		t.Fatalf("total=%d accepted=%d, want both >= 1", s.Total, s.Accepted)
	}
}
