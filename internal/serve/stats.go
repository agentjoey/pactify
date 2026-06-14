package serve

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/stats"
)

func (s *Server) registerStatsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/stats", s.handleStats)
}

// handleStats returns per-task + per-agent work statistics (D1: duration; LOC +
// tokens are best-effort 0 until later increments). A project with no log yet
// returns empty stats (not an error).
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.pmu.RLock()
	p, ok := s.projects[r.PathValue("id")]
	s.pmu.RUnlock()
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	evs, err := event.ReadAll(filepath.Join(p.Path, ".pact", "log.jsonl"))
	if err != nil {
		// uninitialized project (no log) → empty stats, not a 500.
		writeJSON(w, http.StatusOK, stats.Stats{Tasks: []stats.TaskStat{}, Agents: []stats.AgentStat{}})
		return
	}
	writeJSON(w, http.StatusOK, stats.Compute(evs, time.Now().UTC()))
}
