package planner

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/pact"
)

// ApplyTx validates plan, then records every proposed new seat's join (spec §6
// WS-K dynamic seats) and every task assignment as ONE batch acting as seat —
// all-or-nothing, so a rejected plan leaves the ledger untouched.
func ApplyTx(dir string, plan Plan, roster []string, seat string) (assigned int, err error) {
	if err := plan.Validate(roster); err != nil {
		return 0, fmt.Errorf("applytx: %w", err)
	}
	for _, t := range plan.Tasks {
		if _, err := os.Stat(filepath.Join(dir, t.Spec)); err != nil {
			return 0, fmt.Errorf("applytx: spec %q: %w", t.Spec, err)
		}
	}

	p := pact.At(dir).As(seat)

	// Auto-register any proposed new seats before assigning (dynamic seats).
	st, err := p.StateProjection()
	if err != nil {
		return 0, fmt.Errorf("applytx: %w", err)
	}
	onRoster := map[string]bool{}
	for _, a := range st.Agents {
		onRoster[a.ID] = true
	}
	var joins []pact.BatchJoin
	for _, s := range plan.Seats {
		if s.ID == "" || onRoster[s.ID] {
			continue
		}
		joins = append(joins, pact.BatchJoin{
			SeatID: s.ID,
			Roles:  s.Roles,
			Kind:   s.Kind,
		})
		onRoster[s.ID] = true
	}

	var assigns []pact.BatchAssign
	for _, t := range plan.Tasks {
		assigns = append(assigns, pact.BatchAssign{
			TaskID:   t.ID,
			Feature:  plan.Feature,
			Branch:   plan.Branch,
			Owner:    t.Owner,
			Reviewer: t.Reviewer,
			Spec:     t.Spec,
			Deps:     t.Deps,
		})
	}

	n, err := p.ApplyBatch(joins, assigns)
	if err != nil {
		return n, fmt.Errorf("applytx: %w", err)
	}
	return n, nil
}
