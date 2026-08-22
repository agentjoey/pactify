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

// LookupSessionKind is the kind-CHECKED read: a resume id is only interchangeable
// within the kind that minted it, so a record of a DIFFERENT kind for the same
// (seat,task) must be invisible. This is what stops an agy stint being handed a
// codex thread_id as its --conversation after a seat is re-kinded mid-feature
// (dynamic `join --kind`, `--seat-kind`, a fallback-role switch).
func TestLookupSessionKind(t *testing.T) {
	dir := t.TempDir()
	if err := RecordSession(dir, "w", "t1", "codex-cli", "codex-thread"); err != nil {
		t.Fatalf("record: %v", err)
	}

	// Matching kind: hit.
	if id, ok := LookupSessionKind(dir, "w", "t1", "codex-cli"); !ok || id != "codex-thread" {
		t.Fatalf("LookupSessionKind(codex-cli) = (%q,%v), want (codex-thread,true)", id, ok)
	}
	// Different kind, SAME (seat,task): miss. The kind-blind LookupSession still
	// returns it — that difference IS the safeguard.
	if id, ok := LookupSessionKind(dir, "w", "t1", "antigravity"); ok || id != "" {
		t.Fatalf("LookupSessionKind(antigravity) = (%q,%v), want (\"\",false) — "+
			"a codex-cli record must never be served to another kind", id, ok)
	}
	if id, ok := LookupSession(dir, "w", "t1"); !ok || id != "codex-thread" {
		t.Fatalf("sanity: kind-blind LookupSession = (%q,%v), want (codex-thread,true) — "+
			"if this misses too, the test above proves nothing", id, ok)
	}

	// Wrong seat / wrong task / empty store: all miss even with a matching kind.
	if _, ok := LookupSessionKind(dir, "other", "t1", "codex-cli"); ok {
		t.Fatal("a different seat must not match")
	}
	if _, ok := LookupSessionKind(dir, "w", "t2", "codex-cli"); ok {
		t.Fatal("a different task must not match")
	}
	if _, ok := LookupSessionKind(t.TempDir(), "w", "t1", "codex-cli"); ok {
		t.Fatal("an empty store must not match")
	}
	// An empty kind argument must not act as a wildcard.
	if _, ok := LookupSessionKind(dir, "w", "t1", ""); ok {
		t.Fatal("an empty kind must not match a kinded record")
	}

	// A row with a matching kind but no session id is not a hit either.
	if err := writeSessions(sessionsPath(dir), []SessionRecord{
		{Seat: "w", Task: "t3", Kind: "antigravity", SessionID: ""},
	}); err != nil {
		t.Fatalf("seed empty-id row: %v", err)
	}
	if _, ok := LookupSessionKind(dir, "w", "t3", "antigravity"); ok {
		t.Fatal("a row with an empty session id must not be reported as a hit")
	}
}

// Two rows for the same (seat,task) differing only in kind: each kind sees ONLY
// its own. The store is shared by codex-cli, the ACP path and agy, so the kind
// field — not (seat,task) — is what isolates them on the read side.
func TestLookupSessionKind_SameSeatTaskTwoKinds(t *testing.T) {
	dir := t.TempDir()
	// Seeded directly: RecordSession keys by (seat,task) alone and would upsert
	// one row over the other, so it cannot express this state — but the readers
	// must tolerate it (a hand-edited or older-format store).
	if err := writeSessions(sessionsPath(dir), []SessionRecord{
		{Seat: "w", Task: "t1", Kind: "codex-cli", SessionID: "codex-thread"},
		{Seat: "w", Task: "t1", Kind: "antigravity", SessionID: "agy-conv"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if id, ok := LookupSessionKind(dir, "w", "t1", "codex-cli"); !ok || id != "codex-thread" {
		t.Fatalf("codex lookup = (%q,%v), want (codex-thread,true)", id, ok)
	}
	if id, ok := LookupSessionKind(dir, "w", "t1", "antigravity"); !ok || id != "agy-conv" {
		t.Fatalf("agy lookup = (%q,%v), want (agy-conv,true)", id, ok)
	}
	if _, ok := LookupSessionKind(dir, "w", "t1", "kimi-cli"); ok {
		t.Fatal("a third kind must see nothing")
	}
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
