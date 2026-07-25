package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

// fallbackProposalDTO is the dashboard's view of a pending fallback proposal —
// the sidecar an env-class escalation writes next to the escalation record
// (internal/orchestrate/fallback.go). Pending=false means there is nothing to
// approve and the card must not render.
type fallbackProposalDTO struct {
	Pending  bool   `json:"pending"`
	Task     string `json:"task,omitempty"`
	Seat     string `json:"seat,omitempty"`
	FromRole string `json:"fromRole,omitempty"`
	ToRole   string `json:"toRole,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// readFallbackProposal loads the sidecar for dir. Absent, unreadable, or
// malformed all mean "nothing pending" — a proposal is an invitation to act, so
// anything we cannot fully understand must not become a live approve button.
func readFallbackProposal(dir string) (fallbackProposalDTO, bool) {
	b, err := os.ReadFile(filepath.Join(dir, ".pact", "orchestrate", "fallback-proposal.json"))
	if err != nil {
		return fallbackProposalDTO{}, false
	}
	var raw struct {
		Task     string `json:"task"`
		Seat     string `json:"seat"`
		FromRole string `json:"fromRole"`
		ToRole   string `json:"toRole"`
		Reason   string `json:"reason"`
	}
	if json.Unmarshal(b, &raw) != nil || raw.Seat == "" || raw.ToRole == "" {
		return fallbackProposalDTO{}, false
	}
	return fallbackProposalDTO{
		Pending: true, Task: raw.Task, Seat: raw.Seat,
		FromRole: raw.FromRole, ToRole: raw.ToRole, Reason: raw.Reason,
	}, true
}

// handleFallbackProposal reports the project's pending fallback proposal, if any.
func (s *Server) handleFallbackProposal(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	dto, _ := readFallbackProposal(dir)
	writeJSON(w, http.StatusOK, dto)
}

// handleFallbackApprove adopts the pending proposal by resuming the paused run
// WITH --approve-fallback (the driver then runs the named seat under the
// proposed role for that run and clears the sidecar). With nothing pending this
// is a 404, never a silent resume: a stale card must not restart a run.
func (s *Server) handleFallbackApprove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, dir, ok := s.project(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	if _, err := s.actingProject(dir); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if _, pending := readFallbackProposal(dir); !pending {
		writeErr(w, http.StatusNotFound, "no fallback proposal is pending")
		return
	}
	if code, err := s.spawnOrchestrateApprove(dir); err != nil {
		writeErr(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status_url": "/api/projects/" + id + "/orchestrate/status",
	})
}
