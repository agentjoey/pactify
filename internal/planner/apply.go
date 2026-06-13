package planner

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/pact"
)

func Apply(dir string, plan Plan, roster []string) (assigned int, err error) {
	if err := plan.Validate(roster); err != nil {
		return 0, fmt.Errorf("apply: validation failed: %w", err)
	}

	for _, t := range plan.Tasks {
		specPath := filepath.Join(dir, t.Spec)
		if _, err := os.Stat(specPath); err != nil {
			return 0, fmt.Errorf("apply: spec file %q not found", t.Spec)
		}
	}

	for _, t := range plan.Tasks {
		if err := pact.At(dir).As("claude").Assign(t.ID, plan.Feature, plan.Branch, t.Owner, t.Reviewer, t.Spec, t.Deps); err != nil {
			return assigned, fmt.Errorf("apply: assign %s: %w", t.ID, err)
		}
		assigned++
	}
	return assigned, nil
}
