package serve

import (
	"net/http"

	"github.com/agentjoey/pactify/internal/agentreg"
	"github.com/agentjoey/pactify/internal/wizard"
)

// registerSetupRoutes wires the project-setup wizard endpoint (#1): the UI reads
// the proposed seat roster (lead + workers) derived from the machine's registered
// agents, plus any gaps, to drive a "wire my agents into this project" flow.
func (s *Server) registerSetupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup/suggest", s.handleSetupSuggest)
}

type setupBindingDTO struct {
	Seat     string   `json:"seat"`
	Kind     string   `json:"kind"`
	Roles    []string `json:"roles"`
	Drivable bool     `json:"drivable"`
}

type setupSuggestDTO struct {
	Bindings []setupBindingDTO `json:"bindings"`
	Warnings []string          `json:"warnings"`
}

// handleSetupSuggest returns the proposed seat roster from the registered agents.
// Empty registry → an empty roster (the UI prompts the user to register agents).
func (s *Server) handleSetupSuggest(w http.ResponseWriter, _ *http.Request) {
	reg, err := agentreg.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var kinds []string
	for _, a := range reg.Agents {
		kinds = append(kinds, a.Kind)
	}
	bindings := wizard.Suggest(kinds)
	dto := setupSuggestDTO{Bindings: make([]setupBindingDTO, 0, len(bindings)), Warnings: wizard.Validate(bindings)}
	for _, b := range bindings {
		dto.Bindings = append(dto.Bindings, setupBindingDTO{Seat: b.Seat, Kind: b.Kind, Roles: b.Roles, Drivable: b.Drivable})
	}
	writeJSON(w, http.StatusOK, dto)
}
