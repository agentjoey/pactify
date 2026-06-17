package planner

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/projection"
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

func ApplyTx(dir string, plan Plan, roster []string, seat string) (assigned int, err error) {
	if err := plan.Validate(roster); err != nil {
		return 0, fmt.Errorf("applytx: %w", err)
	}
	for _, t := range plan.Tasks {
		if _, err := os.Stat(filepath.Join(dir, t.Spec)); err != nil {
			return 0, fmt.Errorf("applytx: spec %q: %w", t.Spec, err)
		}
	}
	logPath := filepath.Join(dir, ".pact", "log.jsonl")
	orig, _ := os.Stat(logPath)
	var origSize int64
	if orig != nil {
		origSize = orig.Size()
	}
	p := pact.At(dir).As(seat)
	for _, t := range plan.Tasks {
		if err := p.Assign(t.ID, plan.Feature, plan.Branch, t.Owner, t.Reviewer, t.Spec, t.Deps); err != nil {
			_ = os.Truncate(logPath, origSize)
			_ = rerenderState(dir)
			return assigned, fmt.Errorf("applytx: assign %s rolled back: %w", t.ID, err)
		}
		assigned++
	}
	return assigned, nil
}

func rerenderState(dir string) error {
	evs, err := event.ReadAll(paths.LogIn(dir))
	if err != nil {
		return err
	}
	return projection.WriteState(paths.StateIn(dir), projection.Project(evs))
}
