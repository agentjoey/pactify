package pact

import (
	"fmt"

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
