package orchestrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/projection"
	"github.com/agentjoey/pactify/internal/sessions"
)

// agyHomeWith builds a fake ~/.gemini/antigravity-cli containing one conversation
// footprint per id (conversations/<id>.db + sidecars, brain/<id>/, presence lock),
// and points sessions.AntigravityHome at it for the duration of the test.
func agyHomeWith(t *testing.T, ids ...string) string {
	t.Helper()
	home := t.TempDir()
	for _, id := range ids {
		db := filepath.Join(home, "conversations", id+".db")
		if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{db, db + "-wal", db + "-shm"} {
			if err := os.WriteFile(p, []byte("sqlite"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		brain := filepath.Join(home, "brain", id, ".system_generated", "logs")
		if err := os.MkdirAll(brain, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(brain, "transcript.jsonl"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		lock := filepath.Join(home, "presence", id+".lock")
		if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lock, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	orig := sessions.AntigravityHome
	sessions.AntigravityHome = func() string { return home }
	t.Cleanup(func() { sessions.AntigravityHome = orig })
	return home
}

func agyConvExists(home, id string) bool {
	_, err := os.Stat(filepath.Join(home, "conversations", id+".db"))
	return err == nil
}

func agyBrainExists(home, id string) bool {
	_, err := os.Stat(filepath.Join(home, "brain", id))
	return err == nil
}

const (
	agyRecorded = "ee8cb410-7f2e-4382-ba6e-efaed70e6b1f" // pactify launched this one
	agyReviewer = "0622ec88-7856-498e-a9f0-81b5d719bb19" // …and this one
	agyUserOwn  = "d32cb20d-b367-4324-a40c-60a94072434b" // the user started this one by hand
)

// agy has no session CLI and no way to tag a conversation, so cleanup is keyed on
// the conversation ids pactify ITSELF recorded when it launched the stint
// (RecordSession, kind="antigravity"). An accepted task closes exactly those.
func TestCleanupTaskSessions_AntigravityClosesRecordedConversations(t *testing.T) {
	repo := t.TempDir()
	home := agyHomeWith(t, agyRecorded, agyReviewer, agyUserOwn)

	if err := RecordSession(repo, "agy-worker", "t1", "antigravity", agyRecorded); err != nil {
		t.Fatal(err)
	}
	if err := RecordSession(repo, "agy-reviewer", "t1", "antigravity", agyReviewer); err != nil {
		t.Fatal(err)
	}

	notif := &recNotify{}
	opts := Options{
		Dir:        repo,
		SessionRun: func(_, _ string, _ ...string) (string, error) { return "", nil }, // enables cleanup
		SeatKind:   func(string) string { return "antigravity" },
		Notify:     notif,
	}
	opts.cleanupTaskSessions(projection.Task{ID: "t1", Owner: "agy-worker", Reviewer: "agy-reviewer"})

	if agyConvExists(home, agyRecorded) || agyBrainExists(home, agyRecorded) {
		t.Error("owner's recorded agy conversation was not removed")
	}
	if agyConvExists(home, agyReviewer) || agyBrainExists(home, agyReviewer) {
		t.Error("reviewer's recorded agy conversation was not removed")
	}
	if len(notif.msgs) == 0 {
		t.Error("expected a Notify message reporting the agy cleanup")
	}
}

// THE SAFETY INVARIANT. A conversation pactify never created — no store row for
// it — must be untouchable by cleanup, even when it lives in the same agy home
// and even when it belongs to the very same workspace directory pactify drove.
// (This is why cleanup keys on the recorded conversation id and NOT on the
// workspace path baked into the conversation db: workspace matching would sweep
// up the user's own sessions in their own repo — the same reasoning that kept
// CleanupKimiSeat off workDir.)
func TestCleanupTaskSessions_AntigravityNeverTouchesUnrecordedConversations(t *testing.T) {
	repo := t.TempDir()
	home := agyHomeWith(t, agyRecorded, agyUserOwn)

	// Only the worker's conversation is pactify's. agyUserOwn is the human's.
	if err := RecordSession(repo, "agy-worker", "t1", "antigravity", agyRecorded); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		Dir:        repo,
		SessionRun: func(_, _ string, _ ...string) (string, error) { return "", nil },
		SeatKind:   func(string) string { return "antigravity" },
		Notify:     &recNotify{},
	}
	opts.cleanupTaskSessions(projection.Task{ID: "t1", Owner: "agy-worker", Reviewer: "agy-reviewer"})

	if agyConvExists(home, agyRecorded) {
		t.Error("pactify's own conversation should have been removed")
	}
	if !agyConvExists(home, agyUserOwn) {
		t.Fatal("SAFETY VIOLATION: a conversation pactify never created was deleted")
	}
	if !agyBrainExists(home, agyUserOwn) {
		t.Fatal("SAFETY VIOLATION: the user's conversation artifacts were deleted")
	}
}

// A store row minted by a DIFFERENT kind for the same (seat,task) — e.g. the seat
// was re-kinded mid-feature — is not an agy conversation id and must not be used
// as one. LookupSessionKind is the guard; assert it holds end to end.
func TestCleanupTaskSessions_AntigravityIgnoresOtherKindsRecords(t *testing.T) {
	repo := t.TempDir()
	home := agyHomeWith(t, agyRecorded)

	// codex recorded a thread id under the same (seat,task) key.
	if err := RecordSession(repo, "seat", "t1", "codex-cli", agyRecorded); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		Dir:        repo,
		SessionRun: func(_, _ string, _ ...string) (string, error) { return "", nil },
		SeatKind:   func(string) string { return "antigravity" },
		Notify:     &recNotify{},
	}
	opts.cleanupTaskSessions(projection.Task{ID: "t1", Owner: "seat", Reviewer: "seat"})

	if !agyConvExists(home, agyRecorded) {
		t.Fatal("a codex-cli store row must not drive an agy conversation deletion")
	}
}

// No store at all (agy never ran for this task, or the store was already swept)
// is a graceful no-op: nothing deleted, nothing reported, no error.
func TestCleanupTaskSessions_AntigravityEmptyStoreIsNoop(t *testing.T) {
	repo := t.TempDir()
	home := agyHomeWith(t, agyUserOwn)

	notif := &recNotify{}
	opts := Options{
		Dir:        repo,
		SessionRun: func(_, _ string, _ ...string) (string, error) { return "", nil },
		SeatKind:   func(string) string { return "antigravity" },
		Notify:     notif,
	}
	opts.cleanupTaskSessions(projection.Task{ID: "t1", Owner: "agy-worker", Reviewer: "agy-reviewer"})

	if !agyConvExists(home, agyUserOwn) {
		t.Fatal("SAFETY VIOLATION: empty store still deleted a conversation")
	}
	if len(notif.msgs) != 0 {
		t.Errorf("nothing was cleaned; expected no Notify chatter, got %v", notif.msgs)
	}
}

// A store row whose id is empty or malformed must not be turned into a path.
func TestCleanupTaskSessions_AntigravityMalformedIDIsRefused(t *testing.T) {
	repo := t.TempDir()
	home := agyHomeWith(t, agyUserOwn)

	// Hand-write a store row with a hostile id (RecordSession would accept it —
	// the guard belongs at the deletion site).
	p := filepath.Join(repo, ".pact", "orchestrate", "sessions.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal([]SessionRecord{{
		Seat: "agy-worker", Task: "t1", Kind: "antigravity", SessionID: "../..",
	}})
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		Dir:        repo,
		SessionRun: func(_, _ string, _ ...string) (string, error) { return "", nil },
		SeatKind:   func(string) string { return "antigravity" },
		Notify:     &recNotify{},
	}
	opts.cleanupTaskSessions(projection.Task{ID: "t1", Owner: "agy-worker", Reviewer: ""})

	if !agyConvExists(home, agyUserOwn) {
		t.Fatal("SAFETY VIOLATION: a malformed store id caused a deletion")
	}
	if _, err := os.Stat(filepath.Join(home, "conversations")); err != nil {
		t.Fatalf("SAFETY VIOLATION: the conversations dir itself was removed: %v", err)
	}
}

// Cleanup must read the session store BEFORE clearTaskSessionRecords wipes it —
// the accepted-task path calls them in that order (loop.go). Assert the ordering
// dependency explicitly so a future reorder fails loudly instead of silently
// leaking every agy conversation.
func TestCleanupTaskSessions_AntigravityRunsBeforeStoreIsCleared(t *testing.T) {
	repo := t.TempDir()
	home := agyHomeWith(t, agyRecorded)
	if err := RecordSession(repo, "agy-worker", "t1", "antigravity", agyRecorded); err != nil {
		t.Fatal(err)
	}
	task := projection.Task{ID: "t1", Owner: "agy-worker", Reviewer: ""}
	opts := Options{
		Dir:        repo,
		SessionRun: func(_, _ string, _ ...string) (string, error) { return "", nil },
		SeatKind:   func(string) string { return "antigravity" },
		Notify:     &recNotify{},
	}

	// Same order as the accepted branch in loop.go.
	opts.cleanupTaskSessions(task)
	opts.clearTaskSessionRecords(task)

	if agyConvExists(home, agyRecorded) {
		t.Fatal("conversation survived — cleanup must consume the store row before it is cleared")
	}
	if _, ok := LookupSessionKind(repo, "agy-worker", "t1", "antigravity"); ok {
		t.Error("store row should be gone after clearTaskSessionRecords")
	}
}
