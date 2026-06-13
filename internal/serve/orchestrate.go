package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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
