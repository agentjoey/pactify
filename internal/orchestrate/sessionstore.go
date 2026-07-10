package orchestrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/agentjoey/pactify/internal/lockx"
)

// SessionRecord is one persisted ACP session mapping: seat+task identifies the
// stint, kind is the seat's agent kind (for diagnostics), sessionID is the ACP
// session id NewSession minted, and UpdatedAt is the RFC3339 wall time it was last
// (re)written. The set lives in <repoDir>/.pact/orchestrate/sessions.json so a
// retry of the same (seat,task) can LoadSession the prior context instead of
// starting cold (spec driver-modernization §2, Workstream B).
type SessionRecord struct {
	Seat      string `json:"seat"`
	Task      string `json:"task"`
	Kind      string `json:"kind"`
	SessionID string `json:"sessionID"`
	UpdatedAt string `json:"updatedAt"`
}

// sessionsPath is the on-disk store; sessionsLockPath is the flock file that
// serializes read-modify-write across processes/worktrees (mirrors merge's use of
// lockx on the base lock). Both sit under the gitignored runtime dir.
func sessionsPath(repoDir string) string {
	return filepath.Join(repoDir, ".pact", "orchestrate", "sessions.json")
}

func sessionsLockPath(repoDir string) string {
	return filepath.Join(repoDir, ".pact", "orchestrate", "sessions.json.lock")
}

// LoadSessions reads the session store, returning an empty slice when the file is
// absent (a fresh repo) or empty. It does not take the lock — callers that mutate
// go through withSessionsLock, which re-reads under the lock.
func LoadSessions(repoDir string) ([]SessionRecord, error) {
	return readSessions(sessionsPath(repoDir))
}

// readSessions unmarshals the store file; a missing file is not an error.
func readSessions(path string) ([]SessionRecord, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("orchestrate: read sessions: %w", err)
	}
	if len(b) == 0 {
		return nil, nil
	}
	var recs []SessionRecord
	if err := json.Unmarshal(b, &recs); err != nil {
		return nil, fmt.Errorf("orchestrate: parse sessions.json: %w", err)
	}
	return recs, nil
}

// withSessionsLock runs mutate under the cross-process flock, passing it the
// current records and atomically persisting whatever it returns (tmp+rename). The
// whole read-modify-write happens inside the lock so concurrent writers to
// distinct tasks don't clobber each other.
func withSessionsLock(repoDir string, mutate func([]SessionRecord) []SessionRecord) error {
	release, err := lockx.Acquire(context.Background(), sessionsLockPath(repoDir))
	if err != nil {
		return fmt.Errorf("orchestrate: sessions lock: %w", err)
	}
	defer release()

	path := sessionsPath(repoDir)
	recs, err := readSessions(path)
	if err != nil {
		return err
	}
	next := mutate(recs)
	return writeSessions(path, next)
}

// writeSessions persists recs atomically: marshal to a sibling tmp file, then
// rename over the target (rename is atomic within a directory).
func writeSessions(path string, recs []SessionRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("orchestrate: sessions dir: %w", err)
	}
	if recs == nil {
		recs = []SessionRecord{}
	}
	b, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return fmt.Errorf("orchestrate: marshal sessions: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sessions-*.json.tmp")
	if err != nil {
		return fmt.Errorf("orchestrate: sessions tmp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("orchestrate: write sessions tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("orchestrate: close sessions tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("orchestrate: rename sessions: %w", err)
	}
	return nil
}

// RecordSession upserts the session id for (seat,task): an existing entry for the
// pair is replaced (kind/id/timestamp refreshed), otherwise a new one is appended.
// Keyed by (seat,task) so a reviewer and owner sharing a task keep distinct rows.
func RecordSession(repoDir, seat, task, kind, sessionID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	return withSessionsLock(repoDir, func(recs []SessionRecord) []SessionRecord {
		for i := range recs {
			if recs[i].Seat == seat && recs[i].Task == task {
				recs[i].Kind = kind
				recs[i].SessionID = sessionID
				recs[i].UpdatedAt = now
				return recs
			}
		}
		return append(recs, SessionRecord{
			Seat:      seat,
			Task:      task,
			Kind:      kind,
			SessionID: sessionID,
			UpdatedAt: now,
		})
	})
}

// LookupSession returns the recorded ACP session id for (seat,task), or ("",false)
// when none exists (or the store can't be read — resume is best-effort, so a read
// failure degrades to a cold NewSession rather than an error).
func LookupSession(repoDir, seat, task string) (string, bool) {
	recs, err := LoadSessions(repoDir)
	if err != nil {
		return "", false
	}
	for _, r := range recs {
		if r.Seat == seat && r.Task == task && r.SessionID != "" {
			return r.SessionID, true
		}
	}
	return "", false
}

// ClearSession removes the record for (seat,task) if present. It is a no-op (nil
// error) when nothing matches, so terminal-state cleanup can call it blindly.
func ClearSession(repoDir, seat, task string) error {
	return withSessionsLock(repoDir, func(recs []SessionRecord) []SessionRecord {
		out := recs[:0]
		for _, r := range recs {
			if r.Seat == seat && r.Task == task {
				continue
			}
			out = append(out, r)
		}
		return out
	})
}

// RemoveSession removes the record that matches (seat,task,kind). It is a no-op
// when nothing matches, and it checks kind so a stale record from a different
// agent kind cannot accidentally be cleared.
func RemoveSession(repoDir, seat, task, kind string) error {
	return withSessionsLock(repoDir, func(recs []SessionRecord) []SessionRecord {
		out := recs[:0]
		for _, r := range recs {
			if r.Seat == seat && r.Task == task && r.Kind == kind {
				continue
			}
			out = append(out, r)
		}
		return out
	})
}

// PruneSessions drops every record for which keep(seat,task) reports false —
// orchestrate calls it on startup to sweep orphans whose task reached a terminal
// state (accepted/cancelled) while the driver was down.
func PruneSessions(repoDir string, keep func(seat, task string) bool) error {
	return withSessionsLock(repoDir, func(recs []SessionRecord) []SessionRecord {
		out := recs[:0]
		for _, r := range recs {
			if keep(r.Seat, r.Task) {
				out = append(out, r)
			}
		}
		return out
	})
}
