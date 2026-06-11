// Package serve is the pactify HTTP/SSE multi-project dashboard backend.
package serve

import (
	"path/filepath"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/projection"
)

type SeatDTO struct {
	ID    string   `json:"id"`
	Roles []string `json:"roles"`
}

type TaskDTO struct {
	ID       string   `json:"id"`
	Owner    string   `json:"owner"`
	Status   string   `json:"status"`
	Reviewer string   `json:"reviewer"`
	Spec     string   `json:"spec"`
	Evidence string   `json:"evidence"`
	Deps     []string `json:"deps,omitempty"`
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

func logPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".pact", "log.jsonl")
}

// ProjectState reads a project's log and folds the whole log into a JSON DTO.
func ProjectState(projectRoot string) (StateDTO, error) {
	return ProjectStateAt(projectRoot, -1)
}

// ProjectStateAt reads a project's log and folds the first `at` events into a
// JSON DTO. `at` < 0 means "all events"; `at` >= len(evs) clamps to the full
// log. `at` == 0 yields the empty-fold state.
func ProjectStateAt(projectRoot string, at int) (StateDTO, error) {
	evs, err := event.ReadAll(logPath(projectRoot))
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
		d.Agents = append(d.Agents, SeatDTO{ID: a.ID, Roles: a.Roles})
	}
	for _, f := range st.Features {
		fd := FeatureDTO{ID: f.ID, Branch: f.Branch, Status: f.Status}
		for _, t := range f.Tasks {
			ev := ""
			if t.Evidence != nil {
				ev = *t.Evidence
			}
			if t.Status == "awaiting_review" {
				d.AwaitingCount++
			}
			fd.Tasks = append(fd.Tasks, TaskDTO{ID: t.ID, Owner: t.Owner, Status: t.Status, Reviewer: t.Reviewer, Spec: t.Spec, Evidence: ev, Deps: t.Deps})
		}
		d.Features = append(d.Features, fd)
	}
	return d
}
