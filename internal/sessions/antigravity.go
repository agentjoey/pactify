package sessions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// AntigravityHome returns the agy data dir (~/.gemini/antigravity-cli),
// overridable in tests.
//
// Layout probed live against agy 1.1.19 on 2026-08-24 (two cold `agy -p` runs
// plus one `--conversation` resume, diffing the tree before/after). A single
// conversation, identified only by its UUID `conversation_id`, occupies:
//
//	conversations/<id>.db          the transcript store (SQLite; the bulk)
//	conversations/<id>.db-wal      WAL sidecar, frequently left uncheckpointed
//	conversations/<id>.db-shm      shared-memory sidecar
//	brain/<id>/                    artifact dir (.system_generated/logs/transcript*.jsonl,
//	                               .user_uploaded/, scratch/) — the hook payload's
//	                               artifactDirectoryPath
//	presence/<id>.lock             zero-byte liveness marker
//	cache/last_conversations.json  workspace path → most-recent conversation id
//	                               (the index `agy --continue` resolves against)
//
// Nothing in that footprint is a title, label or tag: the conversation db's
// metadata blob carries only the workspace file:// URI, and `cache/
// conversation_metadata.json` (which does have a Title column, always empty, and
// an LLM-written Preview) is a GUI-side cache the headless CLI never writes — the
// probe conversations never appeared in it. `conversation_summaries.db` is the
// same story. That is why cleanup keys on the conversation id and nothing else.
var AntigravityHome = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "antigravity-cli")
}

// IsAntigravity reports whether kind is the agy runner — like kimi, a kind whose
// sessions are cleaned by file ops (CleanupAntigravityConversations) rather than
// a session CLI, because agy has neither.
func IsAntigravity(kind string) bool { return kind == "antigravity" }

// agyConversationIDRe matches a canonical lowercase-or-uppercase UUID, the exact
// shape of agy's conversation_id. Deletion refuses anything else, so a corrupt or
// hostile session-store row can never be joined into a path: "", ".", "..",
// "*", "a/../.." and friends are all rejected before any os.Remove.
var agyConversationIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// CleanupAntigravityConversations removes the on-disk state agy keeps for each of
// ids, and prunes the `--continue` index of any workspace still pointing at one
// of them. It returns the ids it actually removed something for, plus the first
// removal error (best-effort: one failure never stops the rest).
//
// SAFETY — the load-bearing property. This function deletes ONLY what the caller
// names. The caller (orchestrate's cleanupTaskSessions) sources those ids from
// pactify's own session store, where each row was written by the runner from the
// `conversation_id` agy reported for a stint pactify itself launched. A
// conversation the user started by hand has no store row, so its id is never in
// the list, so it is structurally unreachable here. There is deliberately no
// scan-and-match mode: agy exposes no tag pactify could stamp, and the one
// property that IS on disk — the workspace path — is exactly the wrong key, since
// the user's own agy sessions in the repo pactify is driving would match it. That
// is the same reasoning that kept CleanupKimiSeat off workDir.
//
// A missing home (agy never ran on this machine) is a graceful no-op, as is an
// empty/absent id list and an id with nothing on disk.
func CleanupAntigravityConversations(home string, ids []string) (deleted []string, err error) {
	if home == "" || len(ids) == 0 {
		return nil, nil
	}
	if st, e := os.Stat(home); e != nil || !st.IsDir() {
		return nil, nil
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !agyConversationIDRe.MatchString(id) || seen[id] {
			continue
		}
		seen[id] = true
		db := filepath.Join(home, "conversations", id+".db")
		targets := []string{
			db, db + "-wal", db + "-shm",
			filepath.Join(home, "brain", id),
			filepath.Join(home, "presence", id+".lock"),
		}
		removed := false
		for _, p := range targets {
			if _, e := os.Lstat(p); e != nil {
				continue // absent — nothing to do, not an error
			}
			if e := os.RemoveAll(p); e != nil {
				if err == nil {
					err = fmt.Errorf("sessions: remove agy conversation %s: %w", id, e)
				}
				continue
			}
			removed = true
		}
		if removed {
			deleted = append(deleted, id)
		}
	}
	if len(deleted) > 0 {
		pruneAgyContinueIndex(filepath.Join(home, "cache", "last_conversations.json"), deleted)
	}
	return deleted, err
}

// pruneAgyContinueIndex drops the workspace→conversation entries of
// cache/last_conversations.json that point at a conversation we just deleted, so
// a later `agy --continue` from that workspace cold-starts instead of resolving
// to a corpse. Best-effort: a missing, unparseable or unwritable index is left
// exactly as found (the conversation files are already gone, and a stale entry
// resolves to nothing) — same contract as pruneKimiIndex.
func pruneAgyContinueIndex(path string, deleted []string) {
	b, e := os.ReadFile(path)
	if e != nil {
		return
	}
	var idx map[string]string
	if json.Unmarshal(b, &idx) != nil {
		return
	}
	gone := make(map[string]bool, len(deleted))
	for _, id := range deleted {
		gone[id] = true
	}
	changed := false
	for ws, id := range idx {
		if gone[id] {
			delete(idx, ws)
			changed = true
		}
	}
	if !changed {
		return
	}
	out, e := json.Marshal(idx)
	if e != nil {
		return
	}
	_ = os.WriteFile(path, out, 0o600)
}
