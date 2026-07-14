package orchestrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/agentjoey/pactify/internal/projection"
)

// statusHeartbeatInterval is how often a live driver re-stamps status.json so a
// long-but-silent stint doesn't look stale. It MUST stay well under serve's
// stale-run window (10min) so a live run is never mistaken for dead. Overridable
// in tests.
var statusHeartbeatInterval = 3 * time.Minute

// Status is the machine-readable runtime snapshot the orchestrate loop emits to
// .pact/orchestrate/status.json on every iteration and at terminal states.
type Status struct {
	Feature   string `json:"feature"`
	Task      string `json:"task"`
	Seat      string `json:"seat"`
	Action    string `json:"action"`
	Phase     string `json:"phase"`
	Escalated bool   `json:"escalated"`
	Reason    string `json:"reason,omitempty"`
	Done      bool   `json:"done"`
	Total     int    `json:"total"`
	Accepted  int    `json:"accepted"`
	Iter      int    `json:"iter"`
	// FixRound / FixMax surface the pre-review fix-until-green self-repair loop
	// (spec §1 WS-F): when Phase == "fixing" they carry the current round and the
	// bound, so the board can show "fixing n/2". Omitted (0) outside a fix loop.
	FixRound  int    `json:"fix_round,omitempty"`
	FixMax    int    `json:"fix_max,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// buildLoopStatus assembles the per-iteration snapshot for an action (pure, so
// the serial loop and the parallel driver build identical statuses).
func buildLoopStatus(view projection.State, act Action, h History, now func() string) Status {
	total, accepted := progress(view)
	return Status{
		Feature:   act.Feature,
		Task:      act.Task,
		Seat:      act.Seat,
		Action:    actionString(act.Kind),
		Phase:     phaseFor(act),
		Total:     total,
		Accepted:  accepted,
		Iter:      h.Iters,
		UpdatedAt: now(),
	}
}

// buildEscalatedStatus assembles an escalated snapshot, resolving the
// feature/task/seat when task names a real task (vs a feature-level escalation).
func buildEscalatedStatus(view projection.State, task, reason string, h History, now func() string) Status {
	total, accepted := progress(view)
	s := Status{
		Feature:   task,
		Action:    "stuck",
		Phase:     "stuck",
		Escalated: true,
		Reason:    reason,
		Total:     total,
		Accepted:  accepted,
		Iter:      h.Iters,
		UpdatedAt: now(),
	}
	if task != "" {
		for _, f := range view.Features {
			if f.ID == task {
				break
			}
			for _, t := range f.Tasks {
				if t.ID == task {
					s.Feature = f.ID
					s.Task = task
					s.Seat = t.Owner
					break
				}
			}
		}
	}
	return s
}

// buildFixingStatus assembles the `fixing` snapshot the pre-review self-repair
// loop emits while re-running the owner (spec §1 WS-F). Seat is the OWNER doing
// the fix (not the reviewer named in the ActRunReviewer that triggered it). Pure,
// so serial and any future parallel caller build identical statuses.
func buildFixingStatus(view projection.State, act Action, owner string, h History, round, max int, now func() string) Status {
	total, accepted := progress(view)
	return Status{
		Feature:   act.Feature,
		Task:      act.Task,
		Seat:      owner,
		Action:    "run_owner",
		Phase:     "fixing",
		FixRound:  round,
		FixMax:    max,
		Total:     total,
		Accepted:  accepted,
		Iter:      h.Iters,
		UpdatedAt: now(),
	}
}

// parallelStatusDir is where the parallel driver writes one status file per
// in-flight feature, so the dashboard can aggregate concurrent runs (the serial
// status.json holds only one). Kept under the primary tree's orchestrate dir.
func parallelStatusDir(dir string) string {
	return filepath.Join(dir, ".pact", "orchestrate", "parallel")
}

// writeFeatureStatus atomically writes a feature's status to
// <dir>/.pact/orchestrate/parallel/<feature>.json. feature is reduced to a bare
// filename so a stray id can't escape the directory.
func writeFeatureStatus(dir, feature string, s Status) error {
	outDir := parallelStatusDir(dir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	name := filepath.Base(feature)
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid feature id %q", feature)
	}
	final := filepath.Join(outDir, name+".json")
	tmp := final + ".tmp"
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// clearParallelStatus removes stale per-feature status files at the start of a
// parallel run so a fresh run doesn't show a previous run's features.
func clearParallelStatus(dir string) { _ = os.RemoveAll(parallelStatusDir(dir)) }

// --- failure-history persistence ----------------------------------------------

// persistedHistory is the crash-safe slice of History. Fails/LastFail are
// process-internal (an agent crash leaves no ledger event), so a driver restart
// would otherwise hand a persistently-failing task a fresh MaxFails budget
// forever and tripped() would never fire across restarts. Rework is NOT stored:
// it is reconstructed from the ledger's changes_requested events (seedRework).
type persistedHistory struct {
	Fails    map[string]int    `json:"fails"`
	LastFail map[string]string `json:"last_fail"`
}

// historyScopeAll names the history file for an unfiltered serial run (one
// History spans every feature). Scoped runs use the feature id.
const historyScopeAll = "all"

// historyScope maps a serial run's feature filter to its history scope.
func historyScope(feature string) string {
	if feature == "" {
		return historyScopeAll
	}
	return feature
}

// historyPath is <dir>/.pact/orchestrate/history/<scope>.json. The scope is
// reduced to a bare filename so a stray id can't escape the directory (same
// guard as writeFeatureStatus). Kept beside status.json in the runtime dir so
// it survives a sandbox/parallel worktree's teardown.
func historyPath(dir, scope string) string {
	name := filepath.Base(scope)
	if name == "" || name == "." || name == ".." {
		name = historyScopeAll
	}
	return filepath.Join(dir, ".pact", "orchestrate", "history", name+".json")
}

// writeHistory persists h's process-internal counters for scope, atomically
// (temp + rename) like writeStatus. Callers ignore the error: persistence is a
// crash guard, never a loop blocker.
func writeHistory(dir, scope string, h History) error {
	final := historyPath(dir, scope)
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(persistedHistory{Fails: h.Fails, LastFail: h.LastFail})
	if err != nil {
		return err
	}
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// loadHistory merges scope's persisted failure counters into h, so a restarted
// driver resumes the previous run's threshold budget instead of resetting it.
// A missing or unreadable file is a clean start.
func loadHistory(dir, scope string, h *History) {
	b, err := os.ReadFile(historyPath(dir, scope))
	if err != nil {
		return
	}
	var p persistedHistory
	if json.Unmarshal(b, &p) != nil {
		return
	}
	for task, n := range p.Fails {
		h.Fails[task] = n
	}
	for task, cause := range p.LastFail {
		h.LastFail[task] = cause
	}
}

// clearHistory removes scope's persisted failure counters — called when the
// run reaches its terminal done state, so a completed run cannot poison the
// next one's thresholds.
func clearHistory(dir, scope string) { _ = os.Remove(historyPath(dir, scope)) }

// writeStatus atomically writes s to <dir>/.pact/orchestrate/status.json
// (temp file + rename), creating the orchestrate dir if absent.
func writeStatus(dir string, s Status) error {
	outDir := filepath.Join(dir, ".pact", "orchestrate")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create orchestrate dir: %w", err)
	}

	tmp := filepath.Join(outDir, "status.json.tmp")
	final := filepath.Join(outDir, "status.json")

	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal status: %w", err)
	}

	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp status: %w", err)
	}

	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("rename temp status: %w", err)
	}

	return nil
}

// restampStatus bumps status.json's updated_at to now while preserving the rest,
// marking a live-but-silent run as still fresh. It is a no-op when the file is
// missing/unreadable or already terminal (done/escalated) — a terminal run must
// stay terminal so serve's stale guard can retire it.
func restampStatus(dir string, now func() string) {
	b, err := os.ReadFile(filepath.Join(dir, ".pact", "orchestrate", "status.json"))
	if err != nil {
		return
	}
	var s Status
	if json.Unmarshal(b, &s) != nil {
		return
	}
	if s.Done || s.Escalated {
		return
	}
	s.UpdatedAt = statusNow(now)
	_ = writeStatus(dir, s)
}

// heartbeatStatus re-stamps status.json every interval while the driver is alive.
// An agent turn can block up to RunTimeout without the loop writing a new status,
// so a stint longer than serve's stale-run window (10min) would make a LIVE run
// look dead; a concurrent serve restart (which loses the in-memory running-marker)
// then lets a SECOND driver spawn on the same feature (review finding M17). The
// heartbeat keeps a live run visibly fresh. Stops when ctx is cancelled (run
// returns) — so a crashed driver stops heartbeating and IS correctly retired.
func heartbeatStatus(ctx context.Context, dir string, interval time.Duration, now func() string) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			restampStatus(dir, now)
		}
	}
}

// progress computes total tasks and accepted tasks from a state view.
func progress(view projection.State) (total, accepted int) {
	for _, f := range view.Features {
		for _, t := range f.Tasks {
			total++
			if t.Status == "accepted" {
				accepted++
			}
		}
	}
	return
}

func statusNow(now func() string) string {
	if now != nil {
		return now()
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func actionString(kind ActionKind) string {
	switch kind {
	case ActRunOwner:
		return "run_owner"
	case ActRunReviewer:
		return "run_reviewer"
	case ActMerge:
		return "merge"
	case ActStuck:
		return "stuck"
	case ActDone:
		return "done"
	case ActIdle:
		return "idle"
	default:
		return "idle"
	}
}

func phaseFor(act Action) string {
	switch act.Kind {
	case ActRunOwner:
		return "owner working"
	case ActRunReviewer:
		return "reviewer reviewing"
	case ActMerge:
		return "merging feature"
	case ActStuck:
		return "stuck"
	case ActDone:
		return "done"
	case ActIdle:
		return "idle"
	default:
		return "unknown"
	}
}
