// Package wizard turns a machine's registered agents into a proposed project seat
// roster — the bridge from "I registered my agents" to "this project can do
// work" (#1 project setup). Suggest is pure (kinds → bindings) so the CLI and a
// future UI share one heuristic; Validate flags a roster that can't satisfy the
// pact rules (separation of duties needs a worker distinct from the reviewer).
package wizard

import (
	"sort"
	"strings"

	"github.com/agentjoey/pactify/internal/agent"
)

// Binding proposes one project seat: a seat id, the agent kind behind it, the
// roles it should hold, and whether orchestrate can drive it headlessly.
type Binding struct {
	Seat     string
	Kind     string
	Roles    []string
	Drivable bool
}

// seatID derives a stable, friendly seat id from a kind: the kind's first
// dash-segment ("claude-code" → "claude", "gemini-cli" → "gemini", "opencode" →
// "opencode"). Keeps seat ids short and recognizable.
func seatID(kind string) string {
	if i := strings.IndexByte(kind, '-'); i > 0 {
		return kind[:i]
	}
	return kind
}

// isLeadKind reports whether a kind is a natural orchestrator+reviewer lead —
// the claude family, which is the most capable at planning and review.
func isLeadKind(kind string) bool { return strings.HasPrefix(kind, "claude") }

// Suggest proposes a seat roster from registered agent kinds. Heuristic: the lead
// (orchestrator+reviewer) is the first claude-family kind, or — failing that —
// the first kind in sorted order; every other kind is a worker. Output is sorted
// deterministically with the lead first, then workers by kind. Drivable reflects
// whether orchestrate can launch the kind headlessly (#8).
func Suggest(kinds []string) []Binding {
	if len(kinds) == 0 {
		return nil
	}
	sorted := append([]string(nil), kinds...)
	sort.Strings(sorted)

	lead := ""
	for _, k := range sorted {
		if isLeadKind(k) {
			lead = k
			break
		}
	}
	if lead == "" {
		lead = sorted[0] // no claude → first kind leads
	}

	var out []Binding
	out = append(out, Binding{
		Seat: seatID(lead), Kind: lead,
		Roles: []string{"orchestrator", "reviewer"}, Drivable: agent.Drivable(lead),
	})
	for _, k := range sorted {
		if k == lead {
			continue
		}
		out = append(out, Binding{
			Seat: seatID(k), Kind: k,
			Roles: []string{"worker"}, Drivable: agent.Drivable(k),
		})
	}
	return out
}

// Validate returns human-readable warnings for a roster that can't run the pact
// loop cleanly: no orchestrator, no reviewer, or no worker seat distinct from the
// reviewer (a worker can't self-accept, so a reviewer-only roster can't ship).
func Validate(bindings []Binding) []string {
	var hasOrch, hasReviewer, hasWorker bool
	for _, b := range bindings {
		for _, r := range b.Roles {
			switch r {
			case "orchestrator":
				hasOrch = true
			case "reviewer":
				hasReviewer = true
			case "worker":
				hasWorker = true
			}
		}
	}
	var warns []string
	if !hasOrch {
		warns = append(warns, "no orchestrator seat — orchestrate needs one to drive merges")
	}
	if !hasReviewer {
		warns = append(warns, "no reviewer seat — tasks can't be accepted (only a reviewer accepts)")
	}
	if !hasWorker {
		warns = append(warns, "no worker seat — a worker can't self-accept, so the orchestrator/reviewer can't also do the work; add a worker")
	}
	return warns
}
