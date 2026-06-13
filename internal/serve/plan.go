package serve

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/planner"
)

// PlanReviewDTO is the planner-generated task graph surfaced for human review
// before `plan apply` assigns it (#7). Present is false when no plan manifest
// exists for the feature yet. Valid + Error report the machine validation so the
// UI can block "apply" on a structurally broken plan.
type PlanReviewDTO struct {
	Present bool          `json:"present"`
	Feature string        `json:"feature,omitempty"`
	Branch  string        `json:"branch,omitempty"`
	Tasks   []planTaskDTO `json:"tasks,omitempty"`
	Valid   bool          `json:"valid"`
	Error   string        `json:"error,omitempty"`
}

type planTaskDTO struct {
	ID       string   `json:"id"`
	Owner    string   `json:"owner"`
	Reviewer string   `json:"reviewer"`
	Spec     string   `json:"spec"`
	Verify   string   `json:"verify"`
	Deps     []string `json:"deps,omitempty"`
}

func (s *Server) registerPlanRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/plan/{feature}", s.handlePlanReview)
}

// handlePlanReview reads .pact/plan-<feature>.json, parses + validates it against
// the project roster, and returns the task graph for review. A missing manifest
// is a normal "nothing to review yet" (present=false), not an error.
func (s *Server) handlePlanReview(w http.ResponseWriter, r *http.Request) {
	s.pmu.RLock()
	p, ok := s.projects[r.PathValue("id")]
	s.pmu.RUnlock()
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	feature := r.PathValue("feature")

	path := filepath.Join(p.Path, ".pact", "plan-"+feature+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, PlanReviewDTO{Present: false})
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	plan, perr := planner.Parse(b)
	if perr != nil {
		// A malformed manifest still gets surfaced (present, invalid) so the UI can
		// show the parse error rather than silently hiding a broken plan.
		writeJSON(w, http.StatusOK, PlanReviewDTO{Present: true, Feature: feature, Valid: false, Error: perr.Error()})
		return
	}

	dto := PlanReviewDTO{Present: true, Feature: plan.Feature, Branch: plan.Branch, Valid: true}
	for _, t := range plan.Tasks {
		dto.Tasks = append(dto.Tasks, planTaskDTO{
			ID: t.ID, Owner: t.Owner, Reviewer: t.Reviewer, Spec: t.Spec, Verify: t.Verify, Deps: t.Deps,
		})
	}
	// Validate against the project roster so the UI can flag (but still show) a
	// plan that references unknown seats or has a bad dependency.
	if verr := plan.Validate(rosterOf(p.Path)); verr != nil {
		dto.Valid = false
		dto.Error = verr.Error()
	}
	writeJSON(w, http.StatusOK, dto)
}

// rosterOf returns the project's seat ids for plan validation; an unreadable
// state yields an empty roster (Validate then reports unknown-seat references).
func rosterOf(dir string) []string {
	st, err := ProjectStateAt(dir, -1)
	if err != nil {
		return nil
	}
	var ids []string
	for _, a := range st.Agents {
		ids = append(ids, a.ID)
	}
	return ids
}
