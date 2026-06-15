package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/agentcfg"
	"github.com/agentjoey/pactify/internal/agentreg"
)

func (s *Server) registerAgentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents", s.handleAgents)
	mux.HandleFunc("POST /api/agents/{kind}/register", s.handleAgentRegister)
	mux.HandleFunc("DELETE /api/agents/{kind}/register", s.handleAgentUnregister)
	mux.HandleFunc("GET /api/agents/{kind}/config", s.handleAgentConfigGet)
	mux.HandleFunc("POST /api/agents/{kind}/config", s.handleAgentConfigSet)
}

type agentItem struct {
	Kind       string `json:"kind"`
	Installed  bool   `json:"installed"`
	Detail     string `json:"detail"`
	Registered bool   `json:"registered"`
	Label      string `json:"label,omitempty"`
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	scan := agent.Scan()
	reg, err := agentreg.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	regMap := map[string]agentreg.Agent{}
	for _, a := range reg.Agents {
		regMap[a.Kind] = a
	}
	out := make([]agentItem, 0, len(scan))
	for _, sc := range scan {
		a, has := regMap[sc.Kind]
		it := agentItem{
			Kind:       sc.Kind,
			Installed:  sc.Installed,
			Detail:     sc.Detail,
			Registered: has,
		}
		if has {
			it.Label = a.Label
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, out)
}

type agentRegisterReq struct {
	Label string `json:"label"`
}

func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := agent.Get(kind); !ok {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("unknown agent kind %q (supported: %v)", kind, agent.Kinds()))
		return
	}
	var req agentRegisterReq
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	reg, err := agentreg.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	if err := reg.Register(kind, req.Label, ts); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := reg.Save(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"kind": kind})
}

func (s *Server) handleAgentUnregister(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := agent.Get(kind); !ok {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("unknown agent kind %q (supported: %v)", kind, agent.Kinds()))
		return
	}
	reg, err := agentreg.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := reg.Unregister(kind); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := reg.Save(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"kind": kind})
}

type agentConfigDTO struct {
	Kind            string   `json:"kind"`
	Registered      bool     `json:"registered"`
	Drivable        bool     `json:"drivable"`
	Model           string   `json:"model"`
	AllowedTools    []string `json:"allowed_tools"`
	Restricted      bool     `json:"restricted"`
	EffModel        string   `json:"effective_model"`
	EffScoped       bool     `json:"effective_scoped"`
	CandidateModels []string `json:"candidate_models"`
}

func (s *Server) handleAgentConfigGet(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := agent.Get(kind); !ok {
		writeErr(w, http.StatusNotFound, "unknown kind")
		return
	}
	reg, err := agentreg.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	model, tools, restricted := reg.Config(kind)
	dto := agentConfigDTO{
		Kind: kind, Registered: reg.Has(kind), Drivable: agent.Drivable(kind),
		Model: model, AllowedTools: tools, Restricted: restricted,
		CandidateModels: agent.CandidateModels(kind),
	}
	if eff, ok := agentcfg.Resolve(kind); ok {
		dto.EffModel, dto.EffScoped = eff.Model, eff.Scoped
	}
	writeJSON(w, http.StatusOK, dto)
}

type agentConfigReq struct {
	Model        string   `json:"model"`
	AllowedTools []string `json:"allowed_tools"`
	Restricted   bool     `json:"restricted"`
}

func (s *Server) handleAgentConfigSet(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := agent.Get(kind); !ok {
		writeErr(w, http.StatusNotFound, "unknown kind")
		return
	}
	var req agentConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	reg, err := agentreg.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !reg.Has(kind) {
		writeErr(w, http.StatusBadRequest, "agent not registered — register it first")
		return
	}
	if err := reg.SetConfig(kind, req.Model, req.AllowedTools, req.Restricted); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := reg.Save(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	model, tools, restricted := reg.Config(kind)
	dto := agentConfigDTO{
		Kind: kind, Registered: true, Drivable: agent.Drivable(kind),
		Model: model, AllowedTools: tools, Restricted: restricted,
		CandidateModels: agent.CandidateModels(kind),
	}
	if eff, ok := agentcfg.Resolve(kind); ok {
		dto.EffModel, dto.EffScoped = eff.Model, eff.Scoped
	}
	writeJSON(w, http.StatusOK, dto)
}
