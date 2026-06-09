package serve

import (
	"encoding/json"
	"net/http"

	"github.com/agentjoey/pactify/internal/registry"
)

// Server holds the watched projects and the SSE hub.
type Server struct {
	projects map[string]registry.Project // by name (id)
	order    []string                    // stable display order
	hub      *hub
}

// New builds a Server over the given projects.
func New(projects []registry.Project) *Server {
	s := &Server{projects: map[string]registry.Project{}, hub: newHub()}
	for _, p := range projects {
		if _, ok := s.projects[p.Name]; ok {
			continue
		}
		s.projects[p.Name] = p
		s.order = append(s.order, p.Name)
	}
	return s
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/projects", s.handleProjects)
	mux.HandleFunc("GET /api/projects/{id}/state", s.handleState)
	mux.HandleFunc("GET /api/projects/{id}/events", s.handleEvents)
	return mux
}

func (s *Server) handleProjects(w http.ResponseWriter, _ *http.Request) {
	type item struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Path          string `json:"path"`
		Project       string `json:"project"`
		FeatureCount  int    `json:"feature_count"`
		AwaitingCount int    `json:"awaiting_count"`
	}
	out := []item{}
	for _, id := range s.order {
		p := s.projects[id]
		dto, _ := ProjectState(p.Path)
		out = append(out, item{ID: p.Name, Name: p.Name, Path: p.Path, Project: dto.Project, FeatureCount: len(dto.Features), AwaitingCount: dto.AwaitingCount})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projects[r.PathValue("id")]
	if !ok {
		http.Error(w, "unknown project", http.StatusNotFound)
		return
	}
	dto, err := ProjectState(p.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// handleEvents is a temporary stub; replaced by the SSE implementation in Task 3.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
