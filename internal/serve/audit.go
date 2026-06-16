package serve

import (
	"net/http"
	"time"

	"github.com/agentjoey/pactify/internal/audit"
)

func (s *Server) registerAuditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/audit", s.handleAudit)
}

// handleAudit returns audit records for a project, filtered by query params
// (seat, task, session, risk, since=<dur>). Read-only.
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := audit.Filter{
		Project: r.PathValue("id"),
		Seat:    q.Get("seat"),
		Task:    q.Get("task"),
		Session: q.Get("session"),
		Risk:    q.Get("risk"),
	}
	if since := q.Get("since"); since != "" {
		if d, err := time.ParseDuration(since); err == nil {
			f.Since = time.Now().Add(-d)
		}
	}
	recs, err := audit.Query(f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if recs == nil {
		recs = []audit.Record{}
	}
	writeJSON(w, http.StatusOK, recs)
}
