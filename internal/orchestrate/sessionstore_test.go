package orchestrate

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/agentjoey/pactify/internal/projection"
)

// A round trip: record a session, look it up, clear it, and confirm the store
// lands under the gitignored runtime dir as sessions.json.
func TestSessionStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()

	if _, ok := LookupSession(dir, "w", "t1"); ok {
		t.Fatal("empty store should have no record")
	}
	if err := RecordSession(dir, "w", "t1", "kimi-cli", "sess-1"); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, ok := LookupSession(dir, "w", "t1")
	if !ok || got != "sess-1" {
		t.Fatalf("lookup after record: got %q ok=%v", got, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, ".pact", "orchestrate", "sessions.json")); err != nil {
		t.Fatalf("store must persist to .pact/orchestrate/sessions.json: %v", err)
	}

	if err := ClearSession(dir, "w", "t1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := LookupSession(dir, "w", "t1"); ok {
		t.Fatal("record should be gone after clear")
	}
	// Clearing a missing record is a no-op, not an error.
	if err := ClearSession(dir, "w", "t1"); err != nil {
		t.Fatalf("clear of missing record should be a no-op: %v", err)
	}
}

// Recording the same (seat,task) twice upserts (one row, refreshed id) rather than
// appending a duplicate; distinct seats on the same task keep distinct rows.
func TestSessionStoreUpsert(t *testing.T) {
	dir := t.TempDir()
	if err := RecordSession(dir, "w", "t1", "kimi-cli", "sess-1"); err != nil {
		t.Fatal(err)
	}
	if err := RecordSession(dir, "w", "t1", "kimi-cli", "sess-2"); err != nil {
		t.Fatal(err)
	}
	if err := RecordSession(dir, "rev", "t1", "claude-code", "sess-rev"); err != nil {
		t.Fatal(err)
	}
	recs, err := LoadSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("upsert should not duplicate (seat,task): got %d rows %+v", len(recs), recs)
	}
	if got, _ := LookupSession(dir, "w", "t1"); got != "sess-2" {
		t.Fatalf("re-record should refresh the id, got %q", got)
	}
	if got, _ := LookupSession(dir, "rev", "t1"); got != "sess-rev" {
		t.Fatalf("distinct seat should keep its own row, got %q", got)
	}
}

// PruneSessions drops rows the keep func rejects — the terminal-state orphan sweep.
func TestPruneSessions(t *testing.T) {
	dir := t.TempDir()
	_ = RecordSession(dir, "w", "done", "kimi-cli", "s-done")
	_ = RecordSession(dir, "w", "live", "kimi-cli", "s-live")

	// Keep only "live" (simulating "done" reached a terminal state).
	if err := PruneSessions(dir, func(_ /*seat*/, task string) bool { return task == "live" }); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, ok := LookupSession(dir, "w", "done"); ok {
		t.Fatal("terminal task's record should be pruned")
	}
	if got, ok := LookupSession(dir, "w", "live"); !ok || got != "s-live" {
		t.Fatalf("live task's record should survive, got %q ok=%v", got, ok)
	}
}

// pruneOrphanSessions (loop startup hook) drops records whose task is accepted or
// cancelled in the projected state, keeping in-progress ones.
func TestPruneOrphanSessionsByState(t *testing.T) {
	dir := t.TempDir()
	_ = RecordSession(dir, "w", "t-accepted", "kimi-cli", "s1")
	_ = RecordSession(dir, "w", "t-cancelled", "kimi-cli", "s2")
	_ = RecordSession(dir, "w", "t-working", "kimi-cli", "s3")

	st := projection.State{Features: []projection.Feature{{
		ID: "f1",
		Tasks: []projection.Task{
			{ID: "t-accepted", Status: "accepted"},
			{ID: "t-cancelled", Status: "cancelled"},
			{ID: "t-working", Status: "in_progress"},
		},
	}}}
	Options{Dir: dir}.pruneOrphanSessions(st)

	if _, ok := LookupSession(dir, "w", "t-accepted"); ok {
		t.Fatal("accepted task record should be swept")
	}
	if _, ok := LookupSession(dir, "w", "t-cancelled"); ok {
		t.Fatal("cancelled task record should be swept")
	}
	if _, ok := LookupSession(dir, "w", "t-working"); !ok {
		t.Fatal("in-progress task record must survive startup sweep")
	}
}

// clearTaskSessionRecords (accepted-path hook) drops both the owner's and the
// reviewer's rows for a terminal task.
func TestClearTaskSessionRecords(t *testing.T) {
	dir := t.TempDir()
	_ = RecordSession(dir, "w", "t1", "kimi-cli", "s-owner")
	_ = RecordSession(dir, "rev", "t1", "claude-code", "s-rev")
	_ = RecordSession(dir, "w", "t2", "kimi-cli", "s-other")

	Options{Dir: dir}.clearTaskSessionRecords(projection.Task{ID: "t1", Owner: "w", Reviewer: "rev"})

	if _, ok := LookupSession(dir, "w", "t1"); ok {
		t.Fatal("owner record for accepted task should be cleared")
	}
	if _, ok := LookupSession(dir, "rev", "t1"); ok {
		t.Fatal("reviewer record for accepted task should be cleared")
	}
	if _, ok := LookupSession(dir, "w", "t2"); !ok {
		t.Fatal("an unrelated task's record must not be touched")
	}
}

// Concurrent RecordSession calls to DISTINCT tasks must not lose entries — the
// cross-process flock serializes the read-modify-write so neither goroutine
// clobbers the other's append.
func TestSessionStoreConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	const n = 25
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			if err := RecordSession(dir, "a", taskName("a", i), "kimi-cli", "sa"); err != nil {
				t.Errorf("record a: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			if err := RecordSession(dir, "b", taskName("b", i), "kimi-cli", "sb"); err != nil {
				t.Errorf("record b: %v", err)
				return
			}
		}
	}()
	wg.Wait()

	recs, err := LoadSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2*n {
		t.Fatalf("concurrent writes lost entries under lock: got %d want %d", len(recs), 2*n)
	}
	for i := 0; i < n; i++ {
		if _, ok := LookupSession(dir, "a", taskName("a", i)); !ok {
			t.Fatalf("missing seat-a record %d", i)
		}
		if _, ok := LookupSession(dir, "b", taskName("b", i)); !ok {
			t.Fatalf("missing seat-b record %d", i)
		}
	}
}

func taskName(seat string, i int) string {
	return seat + "-t" + string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// RemoveSession drops only the row matching (seat,task,kind); other rows and
// other kinds for the same (seat,task) are left intact.
func TestRemoveSession(t *testing.T) {
	dir := t.TempDir()
	// RecordSession keys by (seat,task) — a later record REPLACES kind+id.
	_ = RecordSession(dir, "w", "t1", "codex-cli", "s-codex")
	_ = RecordSession(dir, "w", "t2", "codex-cli", "s-other")

	if err := RemoveSession(dir, "w", "t1", "codex-cli"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := LookupSession(dir, "w", "t1"); ok {
		t.Fatal("codex-cli record should be removed")
	}
	if _, ok := LookupSession(dir, "w", "t2"); !ok {
		t.Fatal("unrelated task record should survive")
	}

	// Kind-scoped: a row whose kind differs is NOT removed.
	_ = RecordSession(dir, "w", "t1", "kimi-cli", "s-kimi")
	if err := RemoveSession(dir, "w", "t1", "codex-cli"); err != nil {
		t.Fatalf("remove other-kind: %v", err)
	}
	if id, ok := LookupSession(dir, "w", "t1"); !ok || id != "s-kimi" {
		t.Fatalf("kimi row must survive kind-scoped remove, got %q ok=%v", id, ok)
	}

	// Removing a missing (seat,task,kind) is a no-op.
	if err := RemoveSession(dir, "w", "t1", "codex-cli"); err != nil {
		t.Fatalf("remove missing: %v", err)
	}
}
