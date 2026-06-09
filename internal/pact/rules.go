package pact

import (
	"fmt"
	"os"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/projection"
)

func taskExists(st projection.State, taskID string) bool {
	for _, f := range st.Features {
		for _, t := range f.Tasks {
			if t.ID == taskID {
				return true
			}
		}
	}
	return false
}

func findTask(st projection.State, taskID string) (*projection.Task, *projection.Feature) {
	for fi := range st.Features {
		for ti := range st.Features[fi].Tasks {
			if st.Features[fi].Tasks[ti].ID == taskID {
				return &st.Features[fi].Tasks[ti], &st.Features[fi]
			}
		}
	}
	return nil, nil
}

func checkAssign(st projection.State, taskID, owner, reviewer string) error {
	if owner == "" || reviewer == "" {
		return fmt.Errorf("pactify assign: --owner and --reviewer required")
	}
	if owner == reviewer {
		return fmt.Errorf("pactify assign: owner (%s) must differ from reviewer (separation of duties)", owner)
	}
	if taskExists(st, taskID) {
		return fmt.Errorf("pactify assign: task %s already exists", taskID)
	}
	return nil
}

func checkCheckpoint(st projection.State, caller, taskID, evidence string) (*projection.Feature, error) {
	if evidence == "" {
		return nil, fmt.Errorf("pactify checkpoint: --evidence required")
	}
	tk, f := findTask(st, taskID)
	if tk == nil {
		return nil, fmt.Errorf("pactify checkpoint: unknown task %s", taskID)
	}
	if tk.Owner != caller {
		return nil, fmt.Errorf("pactify checkpoint: %s is not the owner of %s (owner: %s)", caller, taskID, tk.Owner)
	}
	return f, nil
}

func checkMerge(st projection.State, feature string) error {
	var feat *projection.Feature
	for fi := range st.Features {
		if st.Features[fi].ID == feature {
			feat = &st.Features[fi]
		}
	}
	if feat == nil || len(feat.Tasks) == 0 {
		return fmt.Errorf("pactify merge: unknown feature %s (or it has no tasks)", feature)
	}
	for _, t := range feat.Tasks {
		if t.Status != "accepted" {
			return fmt.Errorf("pactify merge: cannot merge %s; task %s not accepted", feature, t.ID)
		}
	}
	return nil
}

func featureBranch(st projection.State, feature string) string {
	for _, f := range st.Features {
		if f.ID == feature {
			return f.Branch
		}
	}
	return ""
}

func checkReviewerVerdict(st projection.State, verb, caller, taskID string) (*projection.Feature, error) {
	tk, f := findTask(st, taskID)
	if tk == nil {
		return nil, fmt.Errorf("pactify %s: unknown task %s", verb, taskID)
	}
	if tk.Reviewer != caller {
		return nil, fmt.Errorf("pactify %s: only the reviewer (%s) may review %s; you are %s", verb, tk.Reviewer, taskID, caller)
	}
	if tk.Status != "awaiting_review" {
		return nil, fmt.Errorf("pactify %s: %s is not awaiting_review (status: %s)", verb, taskID, tk.Status)
	}
	return f, nil
}

// ValidateLog runs the v1 conformance checks against the log + rendered STATE.
func ValidateLog() error {
	evs, err := event.ReadAll(paths.Log())
	if err != nil {
		return err
	}
	st := projection.Project(evs)

	// (a) STATE.yml must match a fresh render of the log. A missing/unreadable
	// STATE.yml is itself drift (matches bash, whose diff fails when STATE is absent).
	if b, err := os.ReadFile(paths.State()); err != nil || string(b) != projection.Render(st) {
		return fmt.Errorf("pactify validate: STATE.yml drift vs render(log)")
	}
	declared := map[string]bool{}
	var protocolVersion int
	for _, e := range evs {
		if e.EventType == "init" {
			if seats, ok := e.Payload["seats"].([]any); ok {
				for _, s := range seats {
					if m, ok := s.(map[string]any); ok {
						if id, ok := m["id"].(string); ok {
							declared[id] = true
						}
					}
				}
			}
			if pv, ok := e.Payload["protocol_version"].(float64); ok {
				protocolVersion = int(pv)
			}
		}
	}
	if protocolVersion > paths.ProtocolVersion {
		return fmt.Errorf("pactify validate: protocol_version %d exceeds supported %d; upgrade pactify", protocolVersion, paths.ProtocolVersion)
	}
	for _, e := range evs {
		if e.EventID == "" {
			return fmt.Errorf("pactify validate: event missing event_id")
		}
		if !declared[e.AgentID] {
			return fmt.Errorf("pactify validate: agent_id %q not in seat roster", e.AgentID)
		}
		if !slugRe.MatchString(e.AgentID) {
			return fmt.Errorf("pactify validate: agent_id %q is not a slug", e.AgentID)
		}
	}
	seen := map[string]bool{}
	for _, f := range st.Features {
		for _, t := range f.Tasks {
			if t.Owner == t.Reviewer {
				return fmt.Errorf("pactify validate: rule1 violation (owner==reviewer) in task %s", t.ID)
			}
			if seen[t.ID] {
				return fmt.Errorf("pactify validate: duplicate task id %s", t.ID)
			}
			seen[t.ID] = true
		}
	}
	return nil
}
