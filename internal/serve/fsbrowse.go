package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) registerFsBrowseRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/fs/browse", s.handleFsBrowse)
}

type fsBrowseEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsGit   bool   `json:"isGit"`
	HasPact bool   `json:"hasPact"`
}

type fsBrowseResponse struct {
	Path    string          `json:"path"`
	Parent  string          `json:"parent"`
	Entries []fsBrowseEntry `json:"entries"`
}

func (s *Server) handleFsBrowse(w http.ResponseWriter, r *http.Request) {
	if !s.requireSeat(w) {
		return
	}
	// Confine browsing to a small set of roots — the parent dirs of registered
	// projects (where the add-project wizard needs to look) plus an explicit
	// PACTIFY_FS_ROOT override — instead of the whole filesystem. Without this an
	// authed seat could enumerate ~/.ssh, ~/.aws, /etc, etc. (review finding M2;
	// requireSeat is a process-level config check, not a per-request credential).
	roots := s.fsBrowseRoots()
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = roots[0] // default to the first allowed root, never a bare $HOME
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid path")
		return
	}
	dir = filepath.Clean(abs)
	// Reject a path that escapes every allowed root, or that descends into a hidden
	// directory (~/.ssh, ~/.aws): the latter closes direct entry even when the root
	// is a home dir, matching the dotfile skip already applied to listed children.
	if !withinAnyRoot(dir, roots) || hasHiddenComponent(dir, roots) {
		writeErr(w, http.StatusForbidden, "path is outside the allowed project roots")
		return
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		writeErr(w, http.StatusBadRequest, "path does not exist or is not a directory")
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "cannot read directory")
		return
	}
	var items []fsBrowseEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		full := filepath.Join(dir, name)
		isGit := dirExists(filepath.Join(full, ".git"))
		hasPact := dirExists(filepath.Join(full, ".pact"))
		items = append(items, fsBrowseEntry{Name: name, Path: full, IsGit: isGit, HasPact: hasPact})
	}
	if items == nil {
		items = []fsBrowseEntry{}
	}
	// Only offer an "up" target the client can actually browse — a parent still
	// inside an allowed root. At a root boundary Parent is "" so the UI stops there.
	parent := filepath.Dir(dir)
	if parent == dir || !withinAnyRoot(parent, roots) || hasHiddenComponent(parent, roots) {
		parent = ""
	}
	writeJSON(w, http.StatusOK, fsBrowseResponse{
		Path:    dir,
		Parent:  parent,
		Entries: items,
	})
}

// fsBrowseRoots is the set of directories the fs-browse endpoint may look under:
// the parent dir of every registered project (so the add-project wizard can browse
// where projects live and their siblings) plus any PACTIFY_FS_ROOT overrides
// (os.PathListSeparator-joined). Falls back to $HOME when nothing else is known so
// a fresh install can still add its first project. Always returns ≥1 root.
func (s *Server) fsBrowseRoots() []string {
	seen := map[string]bool{}
	var roots []string
	add := func(p string) {
		if p == "" {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = filepath.Clean(abs)
			if !seen[p] {
				seen[p] = true
				roots = append(roots, p)
			}
		}
	}
	s.pmu.RLock()
	for _, p := range s.projects {
		add(filepath.Dir(p.Path))
	}
	s.pmu.RUnlock()
	for _, p := range filepath.SplitList(os.Getenv("PACTIFY_FS_ROOT")) {
		add(p)
	}
	if len(roots) == 0 {
		if home, err := os.UserHomeDir(); err == nil {
			add(home)
		} else {
			add(".")
		}
	}
	return roots
}

// withinAnyRoot reports whether dir is one of roots or a descendant of one.
func withinAnyRoot(dir string, roots []string) bool {
	for _, root := range roots {
		if rel, err := filepath.Rel(root, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// hasHiddenComponent reports whether dir descends into a hidden (dot-prefixed)
// directory BELOW its containing root — so an authed caller cannot walk directly
// into ~/.ssh or ~/.aws even when the root is a home dir. A dot in the root itself
// is fine; only the path segments beneath it are checked.
func hasHiddenComponent(dir string, roots []string) bool {
	for _, root := range roots {
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue // dir is not under this root
		}
		if rel == "." {
			return false // dir IS the root
		}
		for _, seg := range strings.Split(rel, string(filepath.Separator)) {
			if strings.HasPrefix(seg, ".") {
				return true
			}
		}
		return false // under this root, no hidden segment
	}
	return false
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
