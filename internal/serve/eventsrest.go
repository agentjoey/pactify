package serve

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/agentjoey/pactify/internal/event"
)

// Tail bounds for GET /api/projects/{id}/events/log.
const (
	eventsLogDefaultN = 500
	eventsLogMaxN     = 2000
)

// handleEventsLog returns the last n parsed pact events of the addressed
// ledger as a JSON array — the REST twin of the SSE backfill, carrying the
// same raw event objects the `pact` stream frames do, so clients can reuse
// their PactEvent type. `wt` addresses a worktree's ledger (unknown → 400,
// like handleState); absent → primary. Lines that fail to parse are skipped.
func (s *Server) handleEventsLog(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	n := eventsLogDefaultN
	if raw := r.URL.Query().Get("n"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			writeErr(w, http.StatusBadRequest, "n must be a non-negative integer")
			return
		}
		n = v
	}
	if n > eventsLogMaxN {
		n = eventsLogMaxN
	}
	if wt := r.URL.Query().Get("wt"); wt != "" {
		wp, ok := resolveWorktreePath(dir, wt)
		if !ok {
			writeErr(w, http.StatusBadRequest, "unknown worktree: "+wt)
			return
		}
		dir = wp
	}
	// Emit the raw lines (validated as events) so the payload is byte-identical
	// to what the SSE stream sends.
	out := []json.RawMessage{}
	for _, ln := range tailLog(logPath(dir), n) {
		var ev event.Event
		if json.Unmarshal([]byte(ln), &ev) == nil {
			out = append(out, json.RawMessage(ln))
		}
	}
	writeJSON(w, http.StatusOK, out)
}
