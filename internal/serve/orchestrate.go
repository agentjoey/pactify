package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// OrchestrateStatusDTO wraps the orchestrate runtime snapshot. Present is false
// when the orchestrate driver has never run for this project (no status file).
type OrchestrateStatusDTO struct {
	Present bool            `json:"present"`
	Status  json.RawMessage `json:"status,omitempty"`
}

func orchestrateStatusPath(dir string) string {
	return filepath.Join(dir, ".pact", "orchestrate", "status.json")
}

func (s *Server) registerOrchestrateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/orchestrate/status", s.handleOrchestrateStatus)
	mux.HandleFunc("GET /api/projects/{id}/orchestrate/parallel", s.handleOrchestrateParallel)
}

// ParallelStatusDTO aggregates the per-feature status files a parallel
// orchestrate run writes (one JSON per concurrent feature). Present is false when
// no parallel run has happened (no parallel dir). Features is sorted by id for a
// stable dashboard order.
type ParallelStatusDTO struct {
	Present  bool              `json:"present"`
	Features []json.RawMessage `json:"features"`
}

// handleOrchestrateParallel reads .pact/orchestrate/parallel/*.json and returns
// the per-feature statuses as a list, so the dashboard can show several features
// advancing at once (the single status.json only holds the serial run).
func (s *Server) handleOrchestrateParallel(w http.ResponseWriter, r *http.Request) {
	s.pmu.RLock()
	p, ok := s.projects[r.PathValue("id")]
	s.pmu.RUnlock()
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}

	dir := filepath.Join(p.Path, ".pact", "orchestrate", "parallel")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, ParallelStatusDTO{Present: false})
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // stable order by feature id

	dto := ParallelStatusDTO{Present: true, Features: make([]json.RawMessage, 0, len(names))}
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil || !json.Valid(b) {
			continue // skip a transient half-written file
		}
		dto.Features = append(dto.Features, b)
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) handleOrchestrateStatus(w http.ResponseWriter, r *http.Request) {
	s.pmu.RLock()
	p, ok := s.projects[r.PathValue("id")]
	s.pmu.RUnlock()
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}

	path := orchestrateStatusPath(p.Path)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, OrchestrateStatusDTO{Present: false})
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !json.Valid(b) {
		writeErr(w, http.StatusInternalServerError, "invalid status file")
		return
	}

	writeJSON(w, http.StatusOK, OrchestrateStatusDTO{
		Present: true,
		Status:  b,
	})
}
