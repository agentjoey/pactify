package subpanel

import (
	"net/http"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

// costResponse extends the CLI report without changing its stable JSON contract.
// Quota snapshots are historical text; exhausted routes use current quota memory.
type costResponse struct {
	sub.CostReport
	ExhaustedRoutes []sub.QuotaRoute `json:"exhaustedRoutes"`
}

func (s *Server) cost(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	query := r.URL.Query()
	since, by := "7d", "harness"
	if query.Has("since") {
		since = query.Get("since")
	}
	if query.Has("by") {
		by = query.Get("by")
	}
	start, err := sub.ParseCostSince(since, now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// CostReport validates grouping before reading the store, just as the CLI does.
	report, err := s.store.CostReport(start, by, now)
	if err != nil {
		switch by {
		case "harness", "model", "repo", "day":
			apiError(w, err)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	routes, err := s.store.ExhaustedRoutes(now)
	if err != nil {
		apiError(w, err)
		return
	}
	jsonResponse(w, costResponse{CostReport: report, ExhaustedRoutes: routes})
}
