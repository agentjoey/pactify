package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fallbackProposalDTO is the dashboard's view of ONE pending fallback proposal —
// the sidecar an env-class escalation writes next to the escalation record
// (internal/orchestrate/fallback.go). Scope is the partition the proposal was
// filed under (a feature id, or "all" for an unfiltered serial run); it is the
// filename, not a JSON field, so it is filled in here.
type fallbackProposalDTO struct {
	Scope    string `json:"scope"`
	Task     string `json:"task,omitempty"`
	Seat     string `json:"seat,omitempty"`
	FromRole string `json:"fromRole,omitempty"`
	ToRole   string `json:"toRole,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// fallbackProposalsDTO is the response shape: a LIST, because a
// --max-concurrency > 1 run pauses each feature independently and can leave
// several proposals pending at once. One proposal = one human decision.
type fallbackProposalsDTO struct {
	Proposals []fallbackProposalDTO `json:"proposals"`
}

// fallbackProposalDir is where the driver files one proposal per scope. The
// pre-FALLBACK-PAR single-file path (.pact/orchestrate/fallback-proposal.json)
// is deliberately NOT read: it only ever existed for about a week and never
// actually produced a proposal, and the driver deletes it on the way past.
func fallbackProposalDir(dir string) string {
	return filepath.Join(dir, ".pact", "orchestrate", "fallback")
}

// readFallbackProposals aggregates every pending proposal for dir, sorted by
// scope for a stable card order. Read the same way handleOrchestrateParallel
// reads its per-feature statuses: a missing directory is an empty list, and a
// file that is unreadable or does not decode is SKIPPED (a half-written file, or
// one missing a field an approval acts on). A proposal is an invitation to swap
// agents, so anything we cannot fully understand must not become a live approve
// button.
func readFallbackProposals(dir string) []fallbackProposalDTO {
	entries, err := os.ReadDir(fallbackProposalDir(dir))
	if err != nil {
		return nil
	}
	out := make([]fallbackProposalDTO, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(fallbackProposalDir(dir), e.Name()))
		if err != nil {
			continue
		}
		var raw struct {
			Task     string `json:"task"`
			Seat     string `json:"seat"`
			FromRole string `json:"fromRole"`
			ToRole   string `json:"toRole"`
			Reason   string `json:"reason"`
		}
		if json.Unmarshal(b, &raw) != nil || raw.Seat == "" || raw.ToRole == "" {
			continue
		}
		out = append(out, fallbackProposalDTO{
			Scope: strings.TrimSuffix(e.Name(), ".json"),
			Task:  raw.Task, Seat: raw.Seat,
			FromRole: raw.FromRole, ToRole: raw.ToRole, Reason: raw.Reason,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Scope < out[j].Scope })
	return out
}

// handleFallbackProposal reports the project's pending fallback proposals.
func (s *Server) handleFallbackProposal(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	// Never null: an empty list and "nothing pending" are the same thing, and a
	// null would make the client branch on a second shape for no reason.
	list := readFallbackProposals(dir)
	if list == nil {
		list = []fallbackProposalDTO{}
	}
	writeJSON(w, http.StatusOK, fallbackProposalsDTO{Proposals: list})
}

// handleFallbackApprove adopts ONE named proposal by resuming the paused run
// WITH --approve-fallback <task> (the driver then runs that task's seat under
// the proposed role for that run and clears the sidecar). The task id is the
// key because several proposals can be pending at once and approving is
// per-decision: clicking one card must never adopt another feature's swap.
//
// No pending proposal for that task is a 404, never a silent resume: a stale
// card must not restart a run.
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
	var req struct {
		Task string `json:"task"`
	}
	// A body we cannot parse leaves Task empty, which matches no proposal and
	// falls into the 404 below — the same fail-closed answer as a stale card.
	_ = json.NewDecoder(r.Body).Decode(&req)
	pending := false
	for _, p := range readFallbackProposals(dir) {
		if req.Task != "" && p.Task == req.Task {
			pending = true
			break
		}
	}
	if !pending {
		writeErr(w, http.StatusNotFound, "no fallback proposal is pending for that task")
		return
	}
	if code, err := s.spawnOrchestrateApprove(dir, req.Task); err != nil {
		writeErr(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status_url": "/api/projects/" + id + "/orchestrate/status",
	})
}
