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
