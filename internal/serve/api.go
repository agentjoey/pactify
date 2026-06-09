package serve

import (
	"encoding/json"
	"net/http"

	"github.com/agentjoey/pactify/internal/registry"
	"github.com/fsnotify/fsnotify"
)

// Server holds the watched projects and the SSE hub.
type Server struct {
	projects   map[string]registry.Project // by name (id)
	order      []string                    // stable display order
	hub        *hub
	watcher    *fsnotify.Watcher
	offsets    map[string]int64
	watchPaths []struct{ id, lp string }
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

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.projects[id]; !ok {
		http.Error(w, "unknown project", http.StatusNotFound)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	// Subscribe BEFORE flushing headers so events appended the instant the client
	// sees the response are buffered on the channel, not dropped (avoids a race).
	ch := s.hub.subscribe(id)
	defer s.hub.unsubscribe(id, ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return
			}
			_, _ = w.Write([]byte("event: pact\ndata: " + line + "\n\n"))
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
