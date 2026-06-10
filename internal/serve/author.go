package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
)

// registerAuthorRoutes wires the author (write) endpoints onto mux. These let
// the configured acting seat author tasks and run the assign verb over HTTP.
func (s *Server) registerAuthorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/acting-seat", s.handleActingSeat)
	mux.HandleFunc("POST /api/projects/{id}/tasks", s.handleAuthorTask)
	mux.HandleFunc("POST /api/projects/{id}/verbs/assign", s.handleAuthorAssign)
}

// writeErr emits a {"error":msg} body with the given status code.
func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// project resolves a registered project id to its name + on-disk dir.
func (s *Server) project(id string) (name, dir string, ok bool) {
	p, ok := s.projects[id]
	if !ok {
		return "", "", false
	}
	return p.Name, p.Path, true
}

// actingProject validates the acting seat (configured + present in the
// project's roster) and returns a handle acting as that seat. The roster is
// read from the project's folded state (the seats recorded by the init event);
// a seat not in that roster is rejected. Engine-level rule checks still run on
// every verb — this is the serve-side gate before we ever touch the engine.
func (s *Server) actingProject(dir string) (*pact.Project, error) {
	if s.seat == "" {
		return nil, fmt.Errorf("no acting seat configured (set --seat or PACT_AGENT_ID)")
	}
	dto, err := ProjectState(dir)
	if err != nil {
		return nil, err
	}
	for _, a := range dto.Agents {
		if a.ID == s.seat {
			return pact.At(dir).As(s.seat), nil
		}
	}
	return nil, fmt.Errorf("acting seat %q is not in the project roster", s.seat)
}

func (s *Server) handleActingSeat(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"seat": s.seat})
}

type taskReq struct {
	ID     string `json:"id"`
	SpecMD string `json:"spec_md"`
}

// handleAuthorTask writes a task spec to .pact/tasks/{id}.md. The id must match
// the protocol slug pattern (same as seat ids). Overwrite is allowed: authoring
// is iterative, so re-POSTing the same id re-edits the draft spec.
func (s *Server) handleAuthorTask(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	if _, err := s.actingProject(dir); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	var req taskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !pact.IsSlug(req.ID) {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("task id %q is not a slug", req.ID))
		return
	}
	tasksDir := paths.TasksIn(dir)
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(tasksDir, req.ID+".md"), []byte(req.SpecMD), 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": req.ID})
}

type assignReq struct {
	Task     string   `json:"task"`
	Feature  string   `json:"feature"`
	Branch   string   `json:"branch"`
	Owner    string   `json:"owner"`
	Reviewer string   `json:"reviewer"`
	Spec     string   `json:"spec"`
	Deps     []string `json:"deps"`
}

// handleAuthorAssign runs the assign verb as the acting seat, holding the
// per-project mutex so concurrent author writes don't interleave on the log.
// Engine rule violations surface as 422 with the engine message verbatim.
func (s *Server) handleAuthorAssign(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, dir, ok := s.project(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	proj, err := s.actingProject(dir)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	var req assignReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	mu := s.projectMu(id)
	mu.Lock()
	defer mu.Unlock()
	if err := proj.Assign(req.Task, req.Feature, req.Branch, req.Owner, req.Reviewer, req.Spec, req.Deps); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"task": req.Task})
}
