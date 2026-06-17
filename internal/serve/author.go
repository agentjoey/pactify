package serve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
)

// maxLayoutBytes caps the squad layout sidecar to keep the PUT bounded.
const maxLayoutBytes = 1 << 20 // 1 MiB

// registerAuthorRoutes wires the author (write) endpoints onto mux. These let
// the configured acting seat author tasks and run the assign verb over HTTP.
func (s *Server) registerAuthorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/acting-seat", s.handleActingSeat)
	mux.HandleFunc("POST /api/projects/{id}/tasks", s.handleAuthorTask)
	mux.HandleFunc("POST /api/projects/{id}/verbs/assign", s.handleAuthorAssign)
	mux.HandleFunc("POST /api/projects/{id}/verbs/accept", s.handleAuthorAccept)
	mux.HandleFunc("POST /api/projects/{id}/verbs/changes", s.handleAuthorChanges)
	mux.HandleFunc("POST /api/projects/{id}/verbs/merge", s.handleAuthorMerge)
	mux.HandleFunc("GET /api/projects/{id}/squad/layout", s.handleGetLayout)
	mux.HandleFunc("PUT /api/projects/{id}/squad/layout", s.handlePutLayout)
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
	if !pact.IsSlug(req.ID) {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("task id %q is not a slug", req.ID))
		return
	}
	// A committed task already owns this id — a draft must use a new id, or it
	// would silently overwrite the spec of an assigned task. Drafts (ids not yet
	// assigned to any feature) still re-POST fine.
	if dto, err := ProjectState(dir); err == nil {
		for _, f := range dto.Features {
			for _, t := range f.Tasks {
				if t.ID == req.ID {
					writeErr(w, http.StatusConflict, fmt.Sprintf("task %s already exists; drafts must use a new id", req.ID))
					return
				}
			}
		}
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

// handleAuthorAccept runs the accept verb (reviewer-only) as the acting seat.
func (s *Server) handleAuthorAccept(w http.ResponseWriter, r *http.Request) {
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
	proj, id, ok := s.actingFor(w, r)
	if !ok {
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

// layoutPath is the squad layout sidecar: .pact/squad/layout.json. It is a
// free-form (valid JSON) canvas state stored verbatim — not part of the event
// log, so no mutex and no engine involvement.
func layoutPath(dir string) string {
	return filepath.Join(paths.DirIn(dir), "squad", "layout.json")
}

// handleGetLayout returns the stored layout verbatim, or `{}` when absent.
func (s *Server) handleGetLayout(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	b, err := os.ReadFile(layoutPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			b = []byte("{}")
		} else {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// handlePutLayout stores the request body verbatim at the layout sidecar. The
// body is size-capped at 1 MiB and must be valid JSON. The write is atomic
// (tmp file in the same dir + rename) so a reader never sees a partial file.
func (s *Server) handlePutLayout(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLayoutBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		// MaxBytesReader returns an error once the cap is exceeded.
		writeErr(w, http.StatusRequestEntityTooLarge, "layout exceeds 1MiB")
		return
	}
	if !json.Valid(body) {
		writeErr(w, http.StatusBadRequest, "layout body is not valid JSON")
		return
	}
	dst := layoutPath(dir)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "layout-*.tmp")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, bytes.NewReader(body)); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
