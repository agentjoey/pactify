package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/registry"
)

// AddProject registers a project into the live server: it guards against a
// duplicate name, inserts it into the projects map / display order, and starts a
// per-project fsnotify watch at runtime (no server restart). The shared watcher
// must already be running (StartWatchers); if it isn't, the watch registration
// is a no-op and the project is still served (state/projects work, SSE won't
// stream until a watcher exists — matches the "register only" contract).
// reconcileRegistry brings the live watched set in line with the on-disk
// registry file: entries in the file but not live are added (with a watch),
// entries live but no longer in the file are removed. This is what makes a CLI
// `pactify register` / init/orchestrate auto-register (which only write the
// file) visible on a running serve without a restart — serve watches the file
// and calls this on change (backlog B, 2026-07-23 urbanbricks/tradelinks).
// Matches by absolute path so a rename in the file (same path, new name) drops
// the old name and adds the new. Best-effort per entry: an AddProject failure
// (bad path, dup) is logged, not fatal, so one broken entry can't stall the rest.
func (s *Server) reconcileRegistry() {
	reg, err := registry.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pactify serve: reconcile registry: %v\n", err)
		return
	}
	fileByPath := map[string]registry.Project{}
	for _, p := range reg.Projects {
		fileByPath[filepath.Clean(p.Path)] = p
	}

	// Snapshot the live set under the read lock, then mutate via Add/RemoveProject
	// (each takes the write lock) outside it.
	s.pmu.RLock()
	liveByPath := map[string]registry.Project{}
	for _, p := range s.projects {
		liveByPath[filepath.Clean(p.Path)] = p
	}
	s.pmu.RUnlock()

	for path, p := range fileByPath {
		live, ok := liveByPath[path]
		if ok && live.Name == p.Name {
			continue // unchanged
		}
		if ok && live.Name != p.Name {
			_ = s.RemoveProject(live.Name) // renamed in the file: drop old name
		}
		if err := s.AddProject(p); err != nil {
			fmt.Fprintf(os.Stderr, "pactify serve: reconcile add %q: %v\n", p.Name, err)
		}
	}
	for path, live := range liveByPath {
		if _, stillInFile := fileByPath[path]; !stillInFile {
			_ = s.RemoveProject(live.Name)
		}
	}
}

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
	s.dropStateMemo(name)
	return nil
}

// RenameProject changes a live project's registry name in the in-memory map and
// display order, and rekeys the watch bookkeeping (watchPaths/offsets) to the
// new name — SSE subscribers subscribe by name, so a stale key would keep
// broadcasting under the old name and silently freeze the project's live
// stream. Errors if oldName is unknown or newName is already taken.
func (s *Server) RenameProject(oldName, newName string) error {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	p, ok := s.projects[oldName]
	if !ok {
		return fmt.Errorf("project %q not registered", oldName)
	}
	if _, taken := s.projects[newName]; taken {
		return fmt.Errorf("project %q already registered", newName)
	}
	p.Name = newName
	delete(s.projects, oldName)
	s.projects[newName] = p
	// watchProjectLocked reseeds the offset to the current file size. pmu does
	// NOT stop an external process appending to log.jsonl in this window, so a
	// line landing mid-rename is skipped for live SSE delivery — acceptable: the
	// client refetches full state on reconnect/next event, and state reads fold
	// the whole log regardless.
	s.unwatchProjectLocked(oldName, p.Path)
	s.watchProjectLocked(newName, p.Path)
	for i, id := range s.order {
		if id == oldName {
			s.order[i] = newName
			break
		}
	}
	// The memo is keyed by name; the new name repopulates lazily on next read.
	s.dropStateMemo(oldName)
	return nil
}

// registerRegistryRoutes wires the registry CRUD endpoints onto mux.
func (s *Server) registerRegistryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/registry", s.handleRegistryList)
	mux.HandleFunc("POST /api/registry", s.handleRegistryAdd)
	mux.HandleFunc("PUT /api/registry/{name}", s.handleRegistryRename)
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
	Group  string         `json:"group,omitempty"`
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
		out = append(out, registryItem{Name: p.Name, Path: p.Path, Group: p.Group, Status: s.foldStatus(p.Name, p.Path)})
	}
	writeJSON(w, http.StatusOK, out)
}

// foldStatus computes a project's status. Validity is reported via
// pact.At(dir).Validate(); seats and lastEventTs come from a single (memoized)
// log read so a project that fails strict validation still surfaces useful
// counts where possible.
func (s *Server) foldStatus(name, dir string) registryStatus {
	st := registryStatus{Valid: true}
	if err := pact.At(dir).Validate(); err != nil {
		st.Valid = false
		st.Error = err.Error()
	}
	if dto, evs, err := s.projectStateFull(name, dir); err == nil {
		st.Seats = len(dto.Agents)
		if len(evs) > 0 {
			st.LastEventTs = evs[len(evs)-1].TS
		}
	}
	return st
}

type registerReq struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

// handleRegistryAdd validates a candidate path IN ORDER (absolute → exists+dir →
// git repo → has .pact/ — NO implicit init), persists it to the ~/.pactify
// registry file (Load/Add/Save), then registers + starts a live watcher. A
// duplicate name is 409; validation failures are 400 with the reason.
func (s *Server) handleRegistryAdd(w http.ResponseWriter, r *http.Request) {
	if !s.requireSeat(w) {
		return
	}
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
	if err := reg.Add(req.Name, req.Path, req.Group); err != nil {
		// The path passed the live-set dup check above but the FILE already has it
		// (a CLI/auto register wrote the file, which this running serve never
		// watched — the divergence backlog C names). Don't 409: reconcile so the
		// file entry gets a live watch, then report it as registered.
		s.reconcileRegistry()
		s.pmu.RLock()
		var name string
		for _, p := range s.projects {
			if filepath.Clean(p.Path) == cleaned {
				name = p.Name
				break
			}
		}
		s.pmu.RUnlock()
		if name != "" {
			writeJSON(w, http.StatusOK, map[string]string{"name": name})
			return
		}
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
	if !s.requireSeat(w) {
		return
	}
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

type renameReq struct {
	NewName string `json:"new_name"`
}

// handleRegistryRename renames a project in the live server map and the
// ~/.pactify registry file. The path is preserved, so the project's watch keeps
// streaming. Unknown name is 404; a collision with an existing name is 409.
func (s *Server) handleRegistryRename(w http.ResponseWriter, r *http.Request) {
	if !s.requireSeat(w) {
		return
	}
	old := r.PathValue("name")
	var req renameReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	newName := registry.Slug(req.NewName)
	if newName == "" {
		writeErr(w, http.StatusBadRequest, "new_name is empty")
		return
	}
	if err := s.RenameProject(old, newName); err != nil {
		if strings.Contains(err.Error(), "already registered") {
			writeErr(w, http.StatusConflict, err.Error())
		} else {
			writeErr(w, http.StatusNotFound, err.Error())
		}
		return
	}
	reg, err := registry.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := reg.Rename(old, newName); err == nil {
		if err := reg.Save(); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": newName})
}

// isGitRepo reports whether dir is inside a git working tree, via
// `git rev-parse --git-dir`.
func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	return cmd.Run() == nil
}
