package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeAgyConversation creates the full on-disk footprint agy leaves for one
// conversation id, mirroring the layout probed live against agy 1.1.19
// (2026-08-24): conversations/<id>.db plus its SQLite sidecars, a brain/<id>/
// artifact tree, and a presence/<id>.lock.
func writeAgyConversation(t *testing.T, home, id string) (db, brain, lock string) {
	t.Helper()
	db = filepath.Join(home, "conversations", id+".db")
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{db, db + "-wal", db + "-shm"} {
		if err := os.WriteFile(p, []byte("sqlite"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	brain = filepath.Join(home, "brain", id)
	logs := filepath.Join(brain, ".system_generated", "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "transcript.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock = filepath.Join(home, "presence", id+".lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return db, brain, lock
}

// writeAgyLastConversations writes cache/last_conversations.json, agy's
// workspace-path → most-recent-conversation-id map (the index `agy --continue`
// resolves against).
func writeAgyLastConversations(t *testing.T, home string, m map[string]string) string {
	t.Helper()
	p := filepath.Join(home, "cache", "last_conversations.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func readAgyLastConversations(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("last_conversations.json is not a JSON object: %v (%s)", err, b)
	}
	return m
}

func gone(t *testing.T, path, what string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s still present: %s", what, path)
	}
}

func present(t *testing.T, path, what string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("%s should have survived: %s (%v)", what, path, err)
	}
}

const (
	agyMine  = "ee8cb410-7f2e-4382-ba6e-efaed70e6b1f"
	agyMine2 = "0622ec88-7856-498e-a9f0-81b5d719bb19"
	agyUser  = "d32cb20d-b367-4324-a40c-60a94072434b"
)

func TestIsAntigravity(t *testing.T) {
	if !IsAntigravity("antigravity") {
		t.Error("antigravity should be recognised as the agy file-cleanup kind")
	}
	for _, k := range []string{"kimi-cli", "opencode", "gemini-cli", "claude-code", ""} {
		if IsAntigravity(k) {
			t.Errorf("IsAntigravity(%q) = true, want false", k)
		}
	}
}

// The safety invariant, at the sessions layer: only the ids handed in are
// touched. A conversation the user started themselves (never recorded by
// pactify, so never in the id list) must survive untouched.
func TestCleanupAntigravityConversations_LeavesUnlistedConversationsAlone(t *testing.T) {
	home := t.TempDir()
	mineDB, mineBrain, mineLock := writeAgyConversation(t, home, agyMine)
	userDB, userBrain, userLock := writeAgyConversation(t, home, agyUser)

	deleted, err := CleanupAntigravityConversations(home, []string{agyMine})
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != agyMine {
		t.Fatalf("deleted = %v, want [%s]", deleted, agyMine)
	}

	gone(t, mineDB, "pactify conversation db")
	gone(t, mineDB+"-wal", "pactify conversation wal")
	gone(t, mineDB+"-shm", "pactify conversation shm")
	gone(t, mineBrain, "pactify conversation brain dir")
	gone(t, mineLock, "pactify conversation presence lock")

	present(t, userDB, "user's own conversation db")
	present(t, userDB+"-wal", "user's own conversation wal")
	present(t, userBrain, "user's own brain dir")
	present(t, userLock, "user's own presence lock")
}

func TestCleanupAntigravityConversations_MultipleIDs(t *testing.T) {
	home := t.TempDir()
	a, _, _ := writeAgyConversation(t, home, agyMine)
	b, _, _ := writeAgyConversation(t, home, agyMine2)
	u, _, _ := writeAgyConversation(t, home, agyUser)

	deleted, err := CleanupAntigravityConversations(home, []string{agyMine, agyMine2})
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("deleted = %v, want 2 ids", deleted)
	}
	gone(t, a, "first conversation")
	gone(t, b, "second conversation")
	present(t, u, "user's conversation")
}

// A missing home (agy never ran, or a different machine) is a graceful no-op —
// never an error, mirroring CleanupKimiSeat.
func TestCleanupAntigravityConversations_MissingHomeIsNoop(t *testing.T) {
	deleted, err := CleanupAntigravityConversations(filepath.Join(t.TempDir(), "nope"), []string{agyMine})
	if err != nil {
		t.Fatalf("missing home should be a no-op, got %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("deleted = %v, want none", deleted)
	}
}

func TestCleanupAntigravityConversations_EmptyIDListIsNoop(t *testing.T) {
	home := t.TempDir()
	db, brain, lock := writeAgyConversation(t, home, agyUser)
	for _, ids := range [][]string{nil, {}} {
		deleted, err := CleanupAntigravityConversations(home, ids)
		if err != nil || len(deleted) != 0 {
			t.Fatalf("ids=%v: deleted=%v err=%v, want none/nil", ids, deleted, err)
		}
	}
	present(t, db, "conversation db")
	present(t, brain, "brain dir")
	present(t, lock, "presence lock")
}

// An id that isn't a canonical UUID is refused outright. This is the guard that
// makes a corrupt/hostile store row unable to widen the blast radius: "" or ".."
// or "*" must never be joined into a path and removed.
func TestCleanupAntigravityConversations_RejectsNonUUIDIDs(t *testing.T) {
	home := t.TempDir()
	db, brain, lock := writeAgyConversation(t, home, agyUser)
	convDir := filepath.Join(home, "conversations")
	brainDir := filepath.Join(home, "brain")

	bad := []string{"", ".", "..", "*", "../..", agyUser + "/../..", "  ", "not-a-uuid",
		filepath.Join("..", "..", "etc"), agyUser + ".db"}
	deleted, err := CleanupAntigravityConversations(home, bad)
	if err != nil {
		t.Fatalf("bad ids should be skipped, not error: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("deleted = %v, want none — non-UUID ids must be refused", deleted)
	}
	present(t, db, "conversation db")
	present(t, brain, "brain dir")
	present(t, lock, "presence lock")
	present(t, convDir, "conversations dir")
	present(t, brainDir, "brain dir root")
	present(t, home, "agy home")
}

// An id with no files on disk (already cleaned, or recorded then never
// persisted) reports nothing deleted and no error.
func TestCleanupAntigravityConversations_UnknownIDIsNoop(t *testing.T) {
	home := t.TempDir()
	writeAgyConversation(t, home, agyUser)
	deleted, err := CleanupAntigravityConversations(home, []string{agyMine})
	if err != nil {
		t.Fatalf("unknown id: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("deleted = %v, want none", deleted)
	}
}

// cache/last_conversations.json maps workspace → last conversation id and is what
// `agy --continue` resolves against. Entries pointing at a conversation we just
// deleted must go; every other entry must survive byte-for-byte in meaning.
func TestCleanupAntigravityConversations_PrunesContinueIndex(t *testing.T) {
	home := t.TempDir()
	writeAgyConversation(t, home, agyMine)
	writeAgyConversation(t, home, agyUser)
	idx := writeAgyLastConversations(t, home, map[string]string{
		"/repo/pact-worktree": agyMine,
		"/Users/me/myproject": agyUser,
	})

	if _, err := CleanupAntigravityConversations(home, []string{agyMine}); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	m := readAgyLastConversations(t, idx)
	if _, ok := m["/repo/pact-worktree"]; ok {
		t.Error("continue-index entry for the deleted conversation was not pruned")
	}
	if m["/Users/me/myproject"] != agyUser {
		t.Errorf("user's continue-index entry was damaged: %v", m)
	}
}

// A missing or unparseable continue index is ignored: the conversation files are
// already gone, and a stale index line just resolves to nothing.
func TestCleanupAntigravityConversations_BadContinueIndexIsIgnored(t *testing.T) {
	home := t.TempDir()
	db, _, _ := writeAgyConversation(t, home, agyMine)

	// No index file at all.
	if _, err := CleanupAntigravityConversations(home, []string{agyMine}); err != nil {
		t.Fatalf("missing index should be ignored: %v", err)
	}
	gone(t, db, "conversation db")

	// Garbage index file.
	db2, _, _ := writeAgyConversation(t, home, agyMine2)
	idx := filepath.Join(home, "cache", "last_conversations.json")
	if err := os.MkdirAll(filepath.Dir(idx), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(idx, []byte("not json{{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CleanupAntigravityConversations(home, []string{agyMine2}); err != nil {
		t.Fatalf("garbage index should be ignored: %v", err)
	}
	gone(t, db2, "second conversation db")
	if b, _ := os.ReadFile(idx); string(b) != "not json{{" {
		t.Errorf("unparseable index should be left untouched, got %q", b)
	}
}

// antigravity has no session CLI, so — exactly like kimi — it stays out of the
// `specs` map and the CLI-shaped capability predicates keep reporting false for
// it. Its cleanup route is CleanupAntigravityConversations, gated by
// IsAntigravity, not Manager.CleanupByTitle.
func TestAntigravityHasNoCLICapabilities(t *testing.T) {
	if Supported("antigravity") {
		t.Error("Supported(antigravity) = true; agy has no session list/delete CLI (probed 1.1.19)")
	}
	if CanPrune("antigravity") {
		t.Error("CanPrune(antigravity) = true; agy has no bulk-prune CLI")
	}
	if CanCleanup("antigravity") {
		t.Error("CanCleanup(antigravity) = true; targeted cleanup is file-level, not CLI-level")
	}
}

func TestAntigravityHomeDefaultsUnderGemini(t *testing.T) {
	got := AntigravityHome()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if want := filepath.Join(home, ".gemini", "antigravity-cli"); got != want {
		t.Errorf("AntigravityHome() = %q, want %q", got, want)
	}
}
