package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegacyAPIRemoved(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	h := New().Handler()
	for _, route := range []string{
		"GET /api/projects", "GET /api/projects/p/state", "GET /api/projects/p/worktrees",
		"GET /api/projects/p/events", "GET /api/projects/p/events/log",
		"GET /api/projects/p/orchestrate/status", "GET /api/projects/p/orchestrate/parallel",
		"POST /api/projects/p/orchestrate/run", "POST /api/projects/p/orchestrate/resume",
		"POST /api/projects/p/orchestrate/ship", "GET /api/projects/p/orchestrate/diff",
		"GET /api/projects/p/orchestrate/stream/t", "GET /api/projects/p/fallback-proposal",
		"POST /api/projects/p/fallback-proposal/approve",
		"POST /api/projects/p/cockpit/prompt", "GET /api/projects/p/cockpit/stream",
		"POST /api/projects/p/cockpit/permission", "POST /api/projects/p/cockpit/cancel",
		"GET /api/projects/p/cockpit/status", "POST /api/projects/p/cockpit/resume",
		"GET /api/registry", "POST /api/registry", "PUT /api/registry/p", "DELETE /api/registry/p",
		"GET /api/acting-seat", "POST /api/projects/p/tasks",
		"POST /api/projects/p/verbs/assign", "POST /api/projects/p/verbs/accept",
		"POST /api/projects/p/verbs/changes", "POST /api/projects/p/verbs/merge",
		"GET /api/agents", "GET /api/agents/versions", "POST /api/agents/codex-cli/register",
		"POST /api/agents/codex-cli/test", "GET /api/agents/codex-cli/config", "POST /api/agents/codex-cli/config",
		"GET /api/agents/codex-cli/sessions", "POST /api/agents/codex-cli/sessions/prune",
		"GET /api/projects/p/plan/generate/status", "POST /api/projects/p/plan/generate",
		"GET /api/projects/p/plan/f", "POST /api/projects/p/plan/f/apply",
		"GET /api/setup/suggest", "POST /api/setup/apply", "GET /api/fs/browse",
		"GET /api/projects/p/wiring", "POST /api/projects/p/wiring/codex-cli", "GET /api/projects/p/seats",
		"GET /api/projects/p/stats", "GET /api/recipes", "POST /api/recipes/r/expand",
		"GET /api/manifests", "POST /api/manifests", "DELETE /api/manifests/codex-cli", "GET /api/projects/p/audit",
		"GET /api/projects/p/plan/..%2f..%2fsecret", // former SEC-M3 file-read path
	} {
		t.Run(route, func(t *testing.T) {
			method, path, _ := strings.Cut(route, " ")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if w.Code != http.StatusNotFound {
				t.Fatalf("removed route returned %d: %s", w.Code, w.Body)
			}
		})
	}
}
