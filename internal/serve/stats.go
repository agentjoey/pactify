package serve

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/agentjoey/pactify/internal/diffstat"
	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/projection"
	"github.com/agentjoey/pactify/internal/stats"
	"github.com/agentjoey/pactify/internal/tokens"
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
	res := stats.Compute(evs, time.Now().UTC())
	res = res.WithLOC(locForProject(p.Path, evs))
	tk := tokens.Load(p.Path)
	res = res.WithTokens(tk.Get)
	writeJSON(w, http.StatusOK, res)
}

// locForProject returns a feature→(added,deleted) provider that diffs each
// feature's branch against the project's base branch via git numstat. Failures
// (missing branch, non-git, no base) yield 0 — LOC is best-effort.
func locForProject(repoPath string, evs []event.Event) func(string) (int, int) {
	base := "main"
	for _, e := range evs {
		if e.EventType == "init" {
			if b, ok := e.Payload["base_branch"].(string); ok && b != "" {
				base = b
			}
			break
		}
	}
	branch := map[string]string{} // feature id → branch
	for _, f := range projection.Project(evs).Features {
		if f.Branch != "" {
			branch[f.ID] = f.Branch
		}
	}
	// Only compute LOC for a project that is its OWN git root — a seed living
	// inside this monorepo (dev/showcase) would otherwise diff the outer repo.
	isRoot := diffstat.IsRepoRoot(repoPath)
	return func(feature string) (int, int) {
		b := branch[feature]
		if b == "" || b == base || !isRoot {
			return 0, 0
		}
		// Exclude .pact/* protocol bookkeeping so "code volume" reflects the
		// agent's actual deliverable, not STATE.yml/log churn.
		st, err := diffstat.NumStat(repoPath, base, b, ":(exclude).pact")
		if err != nil {
			return 0, 0
		}
		return st.Added, st.Deleted
	}
}
