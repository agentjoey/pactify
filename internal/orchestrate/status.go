package orchestrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/agentjoey/pactify/internal/projection"
)

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
