package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/registry"
)

// AddProject registers a project into the live server: it guards against a
// duplicate name, inserts it into the projects map / display order, and starts a
// per-project fsnotify watch at runtime (no server restart). The shared watcher
// must already be running (StartWatchers); if it isn't, the watch registration
// is a no-op and the project is still served (state/projects work, SSE won't
// stream until a watcher exists — matches the "register only" contract).
func (s *Server) AddProject(p registry.Project) error {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	if _, ok := s.projects[p.Name]; ok {
		return fmt.Errorf("project %q already registered", p.Name)
	}
	s.projects[p.Name] = p
	s.order = append(s.order, p.Name)
	s.watchProjectLocked(p.Name, p.Path)
	return nil
}

// RemoveProject unregisters a project from the live server: it stops the
// per-project watch, removes it from the projects map and display order. The
// on-disk .pact/ files are never touched (registry-only removal). Unknown name
// is an error.
func (s *Server) RemoveProject(name string) error {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	p, ok := s.projects[name]
	if !ok {
		return fmt.Errorf("project %q not registered", name)
	}
	s.unwatchProjectLocked(name, p.Path)
	delete(s.projects, name)
	for i, id := range s.order {
		if id == name {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return nil
}

// registerRegistryRoutes wires the registry CRUD endpoints onto mux.
func (s *Server) registerRegistryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/registry", s.handleRegistryList)
	mux.HandleFunc("POST /api/registry", s.handleRegistryAdd)
	mux.HandleFunc("DELETE /api/registry/{name}", s.handleRegistryDelete)
}

type registryStatus struct {
	Valid       bool   `json:"valid"`
	Error       string `json:"error,omitempty"`
	Seats       int    `json:"seats"`
	LastEventTs string `json:"lastEventTs,omitempty"`
}

type registryItem struct {
	Name   string         `json:"name"`
	Path   string         `json:"path"`
	Status registryStatus `json:"status"`
}

// handleRegistryList returns each registered project with a content-aware
// status fold: validity (pact.At(dir).Validate() error string), seat count and
// last-event timestamp (from the folded state + raw log).
func (s *Server) handleRegistryList(w http.ResponseWriter, _ *http.Request) {
	s.pmu.RLock()
	projs := make([]registry.Project, 0, len(s.order))
	for _, id := range s.order {
		projs = append(projs, s.projects[id])
	}
	s.pmu.RUnlock()

	out := []registryItem{}
	for _, p := range projs {
		out = append(out, registryItem{Name: p.Name, Path: p.Path, Status: foldStatus(p.Path)})
	}
	writeJSON(w, http.StatusOK, out)
}

// foldStatus computes a project's status. Validity is reported via
// pact.At(dir).Validate(); seats and lastEventTs come from the log so a project
// that fails strict validation still surfaces useful counts where possible.
func foldStatus(dir string) registryStatus {
	st := registryStatus{Valid: true}
	if err := pact.At(dir).Validate(); err != nil {
		st.Valid = false
		st.Error = err.Error()
	}
	if dto, err := ProjectState(dir); err == nil {
		st.Seats = len(dto.Agents)
	}
	if evs, err := event.ReadAll(logPath(dir)); err == nil && len(evs) > 0 {
		st.LastEventTs = evs[len(evs)-1].TS
	}
	return st
}

type registerReq struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// handleRegistryAdd validates a candidate path IN ORDER (absolute → exists+dir →
// git repo → has .pact/ — NO implicit init), persists it to the ~/.pactify
// registry file (Load/Add/Save), then registers + starts a live watcher. A
// duplicate name is 409; validation failures are 400 with the reason.
func (s *Server) handleRegistryAdd(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !filepath.IsAbs(req.Path) {
		writeErr(w, http.StatusBadRequest, "path must be absolute")
		return
	}
	fi, err := os.Stat(req.Path)
	if err != nil || !fi.IsDir() {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("path does not exist or is not a directory: %s", req.Path))
		return
	}
	if !isGitRepo(req.Path) {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("path is not a git repository: %s", req.Path))
		return
	}
	if di, err := os.Stat(filepath.Join(req.Path, ".pact")); err != nil || !di.IsDir() {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("path has no .pact/ directory (run pact init first): %s", req.Path))
		return
	}

	// Reject a path already registered under any (possibly different) name. Two
	// registry entries pointing at the same dir share one fsnotify watch, so
	// removing one would kill the other's live updates — guard before adding.
	cleaned := filepath.Clean(req.Path)
	s.pmu.RLock()
	var dupName string
	for _, p := range s.projects {
		if filepath.Clean(p.Path) == cleaned {
			dupName = p.Name
			break
		}
	}
	s.pmu.RUnlock()
	if dupName != "" {
		writeErr(w, http.StatusConflict, fmt.Sprintf("path already registered as %s", dupName))
		return
	}

	// Persist to the shared registry file so CLI and serve stay consistent.
	reg, err := registry.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := reg.Add(req.Name, req.Path); err != nil {
		// Add's only user-facing failure here is a duplicate name.
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err := reg.Save(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// reg.Add appended the resolved entry; read it back to mirror the registry's
	// name derivation (slug of basename when name is empty).
	added := reg.Projects[len(reg.Projects)-1]
	if err := s.AddProject(added); err != nil {
		// Out-of-band duplicate in the live map (e.g. registered before this
		// server saw the file): surface as 409 and roll back the registry write.
		_ = reg.Remove(added.Name)
		_ = reg.Save()
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": added.Name})
}

// handleRegistryDelete removes a project from the ~/.pactify registry file and
// stops its live watcher (files untouched). Unknown name is 404.
func (s *Server) handleRegistryDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.RemoveProject(name); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	reg, err := registry.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Remove from the file best-effort: the live unregister already succeeded, so
	// a missing file entry is not a client error.
	if err := reg.Remove(name); err == nil {
		if err := reg.Save(); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

// isGitRepo reports whether dir is inside a git working tree, via
// `git rev-parse --git-dir`.
func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	return cmd.Run() == nil
}
