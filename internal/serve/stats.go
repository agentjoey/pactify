package serve

import (
	"net/http"
	"time"

	"github.com/agentjoey/pactify/internal/diffstat"
	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/ledger"
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
	evs, err := ledger.Read(p.Path)
	if err != nil {
		// uninitialized project (no log) → empty stats, not a 500.
		writeJSON(w, http.StatusOK, stats.Stats{Tasks: []stats.TaskStat{}, Agents: []stats.AgentStat{}})
		return
	}
	res := stats.Compute(evs, time.Now().UTC())
	res = res.WithTaskLOC(taskLocForProject(p.Path, evs))
	tk := tokens.Load(p.Path)
	res = res.WithTokens(tk.Get)
	writeJSON(w, http.StatusOK, res)
}

// taskLocForProject returns a taskID→(added,deleted) provider that diffs each
// task's OWN checkpoint commits (subject "pact <taskID>:") against their parents
// via git numstat, excluding .pact bookkeeping. This attributes code volume to
// the task that produced it, instead of showing every task on a branch the same
// branch-level total. Failures (non-root repo, missing branch/commits) yield 0 —
// LOC is best-effort.
func taskLocForProject(repoPath string, evs []event.Event) func(string) (int, int) {
	// Only compute LOC for a project that is its OWN git root — a seed living
	// inside this monorepo (dev/showcase) would otherwise diff the outer repo.
	if !diffstat.IsRepoRoot(repoPath) {
		return func(string) (int, int) { return 0, 0 }
	}
	taskBranch := map[string]string{} // task id → its feature branch
	for _, f := range projection.Project(evs).Features {
		if f.Branch == "" {
			continue
		}
		for _, t := range f.Tasks {
			taskBranch[t.ID] = f.Branch
		}
	}
	cache := map[string][2]int{}
	return func(taskID string) (int, int) {
		if v, ok := cache[taskID]; ok {
			return v[0], v[1]
		}
		res := [2]int{}
		if branch := taskBranch[taskID]; branch != "" {
			// A task's deliverable = the diff of each checkpoint commit it
			// produced (the subject carries the task id), summed.
			if shas, err := diffstat.Commits(repoPath, branch, "pact "+taskID+":"); err == nil {
				for _, sha := range shas {
					st, err := diffstat.NumStat(repoPath, sha+"~1", sha, ":(exclude).pact")
					if err != nil {
						continue
					}
					res[0] += st.Added
					res[1] += st.Deleted
				}
			}
		}
		cache[taskID] = res
		return res[0], res[1]
	}
}
