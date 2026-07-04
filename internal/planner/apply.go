package planner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/projection"
)

// joinNewSeats registers each plan-proposed seat that is not already on the live
// roster with a `join` carrying its declared kind (spec §6 WS-K dynamic seats), so
// a plan that assigns work to a brand-new seat leaves that seat drivable (the
// projection records Agents[].Kind; orchestrate resolves seat→kind from it live).
// Idempotent: a seat already on the roster is skipped, so re-applying a plan does
// not re-join. Each seat joins AS ITSELF (the join event's actor is the seat).
func joinNewSeats(dir string, plan Plan) error {
	if len(plan.Seats) == 0 {
		return nil
	}
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		return err
	}
	onRoster := map[string]bool{}
	for _, a := range st.Agents {
		onRoster[a.ID] = true
	}
	for _, s := range plan.Seats {
		if s.ID == "" || onRoster[s.ID] {
			continue
		}
		if err := pact.At(dir).As(s.ID).JoinKind(s.ID, strings.Join(s.Roles, ","), s.Kind); err != nil {
			return fmt.Errorf("join new seat %s: %w", s.ID, err)
		}
		onRoster[s.ID] = true
	}
	return nil
}

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

	// Auto-register any proposed new seats before assigning to them (dynamic seats).
	if err := joinNewSeats(dir, plan); err != nil {
		return 0, fmt.Errorf("apply: %w", err)
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
	// Auto-register any proposed new seats before assigning (dynamic seats). Done
	// AFTER capturing origSize so a later assign failure's truncate rolls the join
	// events back too, keeping the whole apply atomic.
	if err := joinNewSeats(dir, plan); err != nil {
		_ = os.Truncate(logPath, origSize)
		_ = rerenderState(dir)
		return 0, fmt.Errorf("applytx: %w", err)
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
