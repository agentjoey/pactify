package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/agentreg"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/registry"
	"github.com/agentjoey/pactify/internal/wizard"
)

// registerSetupRoutes wires the project-setup wizard endpoint (#1): the UI reads
// the proposed seat roster (lead + workers) derived from the machine's registered
// agents, plus any gaps, to drive a "wire my agents into this project" flow.
func (s *Server) registerSetupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup/suggest", s.handleSetupSuggest)
	mux.HandleFunc("POST /api/setup/apply", s.handleSetupApply)
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

type setupApplyReq struct {
	Path    string           `json:"path"`
	Project string           `json:"project"`
	Seats   []setupApplySeat `json:"seats"`
	Group   string           `json:"group"`
}

type setupApplySeat struct {
	ID    string   `json:"id"`
	Roles []string `json:"roles"`
	Entry string   `json:"entry"`
	Kind  string   `json:"kind"`
}

type setupApplyWiredResult struct {
	Kind    string `json:"kind"`
	Seat    string `json:"seat"`
	Wrote   bool   `json:"wrote"`
	Path    string `json:"path"`
	DocOnly bool   `json:"docOnly"`
	Snippet string `json:"snippet,omitempty"`
}

type setupApplyResp struct {
	Inited bool                    `json:"inited"`
	Wired  []setupApplyWiredResult `json:"wired"`
	Notes  []string                `json:"notes,omitempty"`
}

func (s *Server) handleSetupApply(w http.ResponseWriter, r *http.Request) {
	if !s.requireSeat(w) {
		return
	}
	var req setupApplyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	fi, err := os.Stat(req.Path)
	if err != nil || !fi.IsDir() {
		writeErr(w, http.StatusBadRequest, "path does not exist or is not a directory")
		return
	}
	if _, err := os.Stat(fmt.Sprintf("%s/.pact", req.Path)); err == nil {
		writeErr(w, http.StatusConflict, "project already initialized (.pact exists)")
		return
	}

	var specs []string
	for _, seat := range req.Seats {
		roles := strings.Join(seat.Roles, ",")
		spec := seat.ID + ":" + roles + ":" + seat.Entry
		if seat.Kind != "" {
			spec += ":" + seat.Kind
		}
		specs = append(specs, spec)
	}

	if err := pact.At(req.Path).As(s.seat).Init(req.Project, specs); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	var wired []setupApplyWiredResult
	var notes []string
	for _, seat := range req.Seats {
		if seat.Kind == "" {
			continue
		}
		a, ok := agent.Get(seat.Kind)
		if !ok {
			notes = append(notes, fmt.Sprintf("unknown kind %q for seat %s — skipped", seat.Kind, seat.ID))
			continue
		}
		roles := strings.Join(seat.Roles, ",")
		wireErr := agent.WireAt(req.Path, seat.Kind, seat.ID, roles, req.Path)
		c := a.Config()
		result := setupApplyWiredResult{
			Kind:    seat.Kind,
			Seat:    seat.ID,
			Path:    c.Path,
			DocOnly: c.Format == agent.TOML,
			Wrote:   wireErr == nil,
		}
		if wireErr != nil {
			notes = append(notes, fmt.Sprintf("wire %s (%s): %v", seat.Kind, seat.ID, wireErr))
		}
		if result.DocOnly {
			_, snippet, _ := agent.Render(seat.Kind, seat.ID, roles, req.Path)
			result.Snippet = snippet
		}
		wired = append(wired, result)
	}

	reg, regErr := registry.Load()
	if regErr == nil {
		if addErr := reg.Add(req.Project, req.Path, req.Group); addErr == nil {
			if saveErr := reg.Save(); saveErr == nil {
				added := reg.Projects[len(reg.Projects)-1]
				_ = s.AddProject(added)
			} else {
				notes = append(notes, fmt.Sprintf("registry save: %v", saveErr))
			}
		} else {
			notes = append(notes, fmt.Sprintf("registry add: %v", addErr))
		}
	} else {
		notes = append(notes, fmt.Sprintf("registry load: %v", regErr))
	}

	writeJSON(w, http.StatusOK, setupApplyResp{Inited: true, Wired: wired, Notes: notes})
}
