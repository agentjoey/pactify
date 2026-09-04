// Package serve is the pactify HTTP/SSE multi-project dashboard backend.
package serve

import (
	"os"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/ledger"
	"github.com/agentjoey/pactify/internal/projection"
)

type SeatDTO struct {
	ID    string   `json:"id"`
	Roles []string `json:"roles"`
	Kind  string   `json:"kind,omitempty"`
}

type TaskDTO struct {
	ID       string   `json:"id"`
	Owner    string   `json:"owner"`
	Status   string   `json:"status"`
	Reviewer string   `json:"reviewer"`
	Spec     string   `json:"spec"`
	Evidence string   `json:"evidence"`
	Deps     []string `json:"deps,omitempty"`
	// Quorum multi-reviewer (spec review-runtime-deepening §2): populated only for a
	// quorum task and omitted otherwise, so a legacy single-reviewer task's DTO JSON
	// stays byte-identical. Reviewer above still carries the first reviewer for
	// back-compat clients (the board badge consumes these — a separate UI task).
	Reviewers []string `json:"reviewers,omitempty"`
	Quorum    int      `json:"quorum,omitempty"`
	Accepts   []string `json:"accepts,omitempty"`
}

type FeatureDTO struct {
	ID     string    `json:"id"`
	Branch string    `json:"branch"`
	Status string    `json:"status"`
	Tasks  []TaskDTO `json:"tasks"`
}

type StateDTO struct {
	Project       string       `json:"project"`
	Agents        []SeatDTO    `json:"agents"`
	Features      []FeatureDTO `json:"features"`
	AwaitingCount int          `json:"awaiting_count"`
}

// logPath is serve's project-scoped ledger path. It delegates to internal/ledger
// so this package has exactly one answer to "where is project X's log", and so
// the reason it must NOT be paths.LogIn lives with the rule (see
// TestServeNeverResolvesLedgerPathsFromProcessEnv).
func logPath(projectRoot string) string { return ledger.Path(projectRoot) }

// splitNonEmptyLines splits s on newlines, trimming \r and dropping blanks.
func splitNonEmptyLines(s string) []string {
	out := []string{}
	for _, ln := range strings.Split(s, "\n") {
		if ln = strings.TrimRight(ln, "\r"); ln != "" {
			out = append(out, ln)
		}
	}
	return out
}

// tailLog returns the last n non-empty lines of the log at lp (oldest→newest).
// Missing/empty file → nil. Used to backfill a new SSE subscriber with recent
// history so Live shows the log.jsonl tail on open, not just events that arrive
// after connect. log.jsonl is small for dashboards, so a whole-file read is
// fine; switch to a reverse chunk read if logs ever grow large.
func tailLog(lp string, n int) []string {
	b, err := os.ReadFile(lp)
	if err != nil {
		return nil
	}
	lines := splitNonEmptyLines(string(b))
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// ProjectState reads a project's log and folds the whole log into a JSON DTO.
func ProjectState(projectRoot string) (StateDTO, error) {
	return ProjectStateAt(projectRoot, -1)
}

// stateMemo is one project's cached full-fold read: the folded DTO plus the
// parsed events it was folded from, stamped with the log file identity
// (path, size, mtime) it was read at.
type stateMemo struct {
	logPath string
	size    int64
	modTime time.Time
	dto     StateDTO
	evs     []event.Event
}

// projectStateFull returns the full-fold state DTO plus the parsed events it
// was folded from, memoized per project id. The memo covers ONLY this case —
// prefix folds (`at`) and worktree reads (`wt`) go through ProjectStateAt. A
// hit requires the log's path, size and mtime to all match the cached stamp;
// any mismatch (or a missing log) refolds from disk, so external writers that
// bypass fsnotify still surface on the next read. Callers must treat both
// return values as read-only: they are shared across requests.
func (s *Server) projectStateFull(id, projectRoot string) (StateDTO, []event.Event, error) {
	lp := logPath(projectRoot)
	fi, err := os.Stat(lp)
	if err != nil {
		// No stat, no stamp: read uncached (ReadAll folds a missing file to the
		// empty state, matching ProjectState).
		evs, rerr := event.ReadAll(lp)
		if rerr != nil {
			return StateDTO{}, nil, rerr
		}
		return toDTO(projection.Project(evs)), evs, nil
	}
	s.memoMu.Lock()
	m, ok := s.stateMemos[id]
	s.memoMu.Unlock()
	if ok && m.logPath == lp && m.size == fi.Size() && m.modTime.Equal(fi.ModTime()) {
		return m.dto, m.evs, nil
	}
	evs, err := event.ReadAll(lp)
	if err != nil {
		return StateDTO{}, nil, err
	}
	dto := toDTO(projection.Project(evs))
	// The stamp was taken before the read: if an append landed in between, the
	// cached content is NEWER than the stamp, so the next stat mismatches and
	// refolds — never serves stale.
	s.memoMu.Lock()
	if s.stateMemos == nil {
		s.stateMemos = map[string]stateMemo{}
	}
	s.stateMemos[id] = stateMemo{logPath: lp, size: fi.Size(), modTime: fi.ModTime(), dto: dto, evs: evs}
	s.memoMu.Unlock()
	return dto, evs, nil
}

// dropStateMemo forgets a project's cached full-fold state so a removed or
// renamed id doesn't linger in the memo.
func (s *Server) dropStateMemo(id string) {
	s.memoMu.Lock()
	delete(s.stateMemos, id)
	s.memoMu.Unlock()
}

// ProjectStateAt reads a project's log and folds the first `at` events into a
// JSON DTO. `at` < 0 means "all events"; `at` >= len(evs) clamps to the full
// log. `at` == 0 yields the empty-fold state.
func ProjectStateAt(projectRoot string, at int) (StateDTO, error) {
	evs, err := ledger.Read(projectRoot)
	if err != nil {
		return StateDTO{}, err
	}
	if at >= 0 && at < len(evs) {
		evs = evs[:at]
	}
	return toDTO(projection.Project(evs)), nil
}

func toDTO(st projection.State) StateDTO {
	// Initialize the slices so an empty roster/feature list marshals as JSON `[]`
	// rather than `null` (Go marshals a nil slice as null). The dashboard's State
	// type promises non-null arrays; a freshly-registered repo with agents but no
	// features would otherwise crash the canvas client-side.
	d := StateDTO{Project: st.Project, Agents: []SeatDTO{}, Features: []FeatureDTO{}}
	for _, a := range st.Agents {
		d.Agents = append(d.Agents, SeatDTO{ID: a.ID, Roles: a.Roles, Kind: a.Kind})
	}
	for _, f := range st.Features {
		// Tasks seeded for the same []-not-null contract as Agents/Features above.
		fd := FeatureDTO{ID: f.ID, Branch: f.Branch, Status: f.Status, Tasks: []TaskDTO{}}
		for _, t := range f.Tasks {
			ev := ""
			if t.Evidence != nil {
				ev = *t.Evidence
			}
			if t.Status == "awaiting_review" {
				d.AwaitingCount++
			}
			fd.Tasks = append(fd.Tasks, TaskDTO{ID: t.ID, Owner: t.Owner, Status: t.Status, Reviewer: t.Reviewer, Spec: t.Spec, Evidence: ev, Deps: t.Deps, Reviewers: t.Reviewers, Quorum: t.Quorum, Accepts: t.Accepts})
		}
		d.Features = append(d.Features, fd)
	}
	return d
}
