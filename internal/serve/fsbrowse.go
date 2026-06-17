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
	dir := r.URL.Query().Get("path")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		dir = home
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
	writeJSON(w, http.StatusOK, fsBrowseResponse{
		Path:    dir,
		Parent:  filepath.Dir(dir),
		Entries: items,
	})
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
