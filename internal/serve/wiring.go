package serve

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/pact"
)

// registerWiringRoutes wires the agent-wiring endpoints onto mux. Wiring is a
// setup-class operation (configuring a repo for an agent), so — unlike the
// author verbs — it carries NO acting-seat requirement.
func (s *Server) registerWiringRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/wiring", s.handleWiringList)
	mux.HandleFunc("POST /api/projects/{id}/wiring/{kind}", s.handleWire)
}

// handleWiringList returns the content-aware wiring status of every registry
// kind for the project's on-disk dir (agent.ProbeWiring — shared with doctor).
func (s *Server) handleWiringList(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	writeJSON(w, http.StatusOK, agent.ProbeWiring(dir))
}

type wireReq struct {
	Seat  string `json:"seat"`
	Roles string `json:"roles"`
}

type wireResp struct {
	Wrote   bool   `json:"wrote"`
	Global  bool   `json:"global"`
	Path    string `json:"path"`
	DocOnly bool   `json:"docOnly"`
	Snippet string `json:"snippet,omitempty"`
}

// handleWire runs agent.Wire(kind, seat, roles, dir): it bakes the kind's entry
// block and, for JSON kinds, merges the pact server into the agent config.
// TOML (codex) kinds are doc-only — the config is not written; the response
// carries docOnly:true and the snippet from agent.Render so the UI can show it.
// Validations, in order: unknown kind → 400 (lists supported kinds); seat must
// be a protocol slug; roles must be non-empty.
func (s *Server) handleWire(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	if _, err := s.actingProject(dir); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	kind := r.PathValue("kind")
	a, ok := agent.Get(kind)
	if !ok {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("unknown agent kind %q (supported: %v)", kind, agent.Kinds()))
		return
	}
	var req wireReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !pact.IsSlug(req.Seat) {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("seat %q is not a slug", req.Seat))
		return
	}
	if req.Roles == "" {
		writeErr(w, http.StatusBadRequest, "roles is required")
		return
	}

	if err := agent.WireAt(dir, kind, req.Seat, req.Roles, dir); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	c := a.Config()
	resp := wireResp{
		Global:  c.Scope == agent.Global,
		Path:    c.Path,
		DocOnly: c.Format == agent.TOML,
		Wrote:   true,
	}
	if resp.DocOnly {
		// Wire wrote no config for doc-only kinds (only the entry block, if any).
		// Surface the config snippet for the UI to display / copy.
		_, snippet, err := agent.Render(kind, req.Seat, req.Roles, dir)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp.Snippet = snippet
	}
	writeJSON(w, http.StatusOK, resp)
}
