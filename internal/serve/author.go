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
	mux.HandleFunc("POST /api/projects/{id}/verbs/accept", s.handleAuthorAccept)
	mux.HandleFunc("POST /api/projects/{id}/verbs/changes", s.handleAuthorChanges)
	mux.HandleFunc("POST /api/projects/{id}/verbs/merge", s.handleAuthorMerge)
}

// writeErr emits a {"error":msg} body with the given status code.
func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// project resolves a registered project id to its name + on-disk dir.
func (s *Server) project(id string) (name, dir string, ok bool) {
	s.pmu.RLock()
	p, ok := s.projects[id]
	s.pmu.RUnlock()
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

func (s *Server) requireSeat(w http.ResponseWriter) bool {
	if s.seat == "" {
		writeErr(w, http.StatusUnprocessableEntity, "no acting seat configured (set --seat or PACT_AGENT_ID)")
		return false
	}
	return true
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
	if code, err := authorTask(dir, req.ID, req.SpecMD); err != nil {
		writeErr(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": req.ID})
}

// authorTask writes a task draft spec to .pact/tasks/{id}.md — the shared core of
// the dashboard's HTTP endpoint and the remote pact.task rpc. The id must be a
// protocol slug; re-writing a draft id is allowed (authoring is iterative) but an
// id already committed to a feature is rejected (would clobber an assigned task's
// spec). Returns (httpStatusCode, error).
func authorTask(dir, id, specMD string) (int, error) {
	if !pact.IsSlug(id) {
		return http.StatusBadRequest, fmt.Errorf("task id %q is not a slug", id)
	}
	if dto, err := ProjectState(dir); err == nil {
		for _, f := range dto.Features {
			for _, t := range f.Tasks {
				if t.ID == id {
					return http.StatusConflict, fmt.Errorf("task %s already exists; drafts must use a new id", id)
				}
			}
		}
	}
	tasksDir := paths.TasksIn(dir)
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return http.StatusInternalServerError, err
	}
	if err := os.WriteFile(filepath.Join(tasksDir, id+".md"), []byte(specMD), 0o644); err != nil {
		return http.StatusInternalServerError, err
	}
	return 0, nil
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

// verbReq carries the body shared by the reviewer-verdict and merge verbs. Only
// the relevant fields are read per handler.
type verbReq struct {
	Task    string `json:"task"`
	Reason  string `json:"reason"`
	Feature string `json:"feature"`
}

// actingFor resolves the project + acting handle for a write verb, writing the
// appropriate error and returning ok=false when the project is unknown (404) or
// the acting seat is invalid (422).
func (s *Server) actingFor(w http.ResponseWriter, r *http.Request) (*pact.Project, string, bool) {
	id := r.PathValue("id")
	_, dir, ok := s.project(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return nil, "", false
	}
	proj, err := s.actingProject(dir)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return nil, "", false
	}
	return proj, id, true
}

// reviewerOf returns the reviewer seat of taskID in dir's current state ("" if not
// found) — used to detect human-in-the-loop tasks whose reviewer is "human".
func reviewerOf(dir, taskID string) string {
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		return ""
	}
	for _, f := range st.Features {
		for _, t := range f.Tasks {
			if t.ID == taskID {
				return t.Reviewer
			}
		}
	}
	return ""
}

// actingSeatForReview resolves the project handle for a review verb (accept/
// changes) on taskID. "human" is a RESERVED reviewer for human-in-the-loop tasks:
// it is not a roster seat, so the authenticated dashboard user acts as "human"
// (gated by requireSeat + the front door's Cloudflare Access, NOT roster
// membership). Any other reviewer goes through the normal acting-seat path (the
// configured seat, which must be in the roster).
func (s *Server) actingSeatForReview(w http.ResponseWriter, dir, taskID string) (*pact.Project, bool) {
	if reviewerOf(dir, taskID) == "human" {
		if !s.requireSeat(w) {
			return nil, false
		}
		return pact.At(dir).As("human"), true
	}
	proj, err := s.actingProject(dir)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return nil, false
	}
	return proj, true
}

// handleAuthorAccept runs the accept verb (reviewer-only). For a human-reviewed
// task the dashboard user accepts as "human"; otherwise as the acting seat.
func (s *Server) handleAuthorAccept(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, dir, ok := s.project(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	var req verbReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	proj, ok := s.actingSeatForReview(w, dir, req.Task)
	if !ok {
		return
	}
	mu := s.projectMu(id)
	mu.Lock()
	defer mu.Unlock()
	if err := proj.Accept(req.Task); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"task": req.Task})
}

// handleAuthorChanges runs the changes_requested verb (reviewer-only). The
// engine does not enforce a non-empty reason, so the API rejects an empty
// reason with 400 before touching the log.
func (s *Server) handleAuthorChanges(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, dir, ok := s.project(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	var req verbReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Reason == "" {
		writeErr(w, http.StatusBadRequest, "reason is required")
		return
	}
	proj, ok := s.actingSeatForReview(w, dir, req.Task)
	if !ok {
		return
	}
	mu := s.projectMu(id)
	mu.Lock()
	defer mu.Unlock()
	if err := proj.Changes(req.Task, req.Reason); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"task": req.Task})
}

// handleAuthorMerge runs the merge verb as the acting seat.
func (s *Server) handleAuthorMerge(w http.ResponseWriter, r *http.Request) {
	proj, id, ok := s.actingFor(w, r)
	if !ok {
		return
	}
	var req verbReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	mu := s.projectMu(id)
	mu.Lock()
	defer mu.Unlock()
	if err := proj.Merge(req.Feature); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"feature": req.Feature})
}

