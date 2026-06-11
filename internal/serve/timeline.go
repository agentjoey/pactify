package serve

import (
	"net/http"
	"strconv"

	"github.com/agentjoey/pactify/internal/event"
)

// TimelineEventDTO is one entry in the timeline index: a 1-based position plus
// the event's envelope fields. task/feature are included only when the event
// carries them. No payload is exposed.
type TimelineEventDTO struct {
	N       int    `json:"n"`
	TS      string `json:"ts"`
	Type    string `json:"type"`
	Actor   string `json:"actor"`
	Task    string `json:"task,omitempty"`
	Feature string `json:"feature,omitempty"`
}

// TimelineDTO is the timeline index for a project: the total event count and
// the per-event envelope rows in append order.
type TimelineDTO struct {
	Total  int                `json:"total"`
	Events []TimelineEventDTO `json:"events"`
}

// registerTimelineRoutes wires the read-only timeline index endpoint onto mux.
// The state?at=N prefix-fold is served by the existing handleState handler.
func (s *Server) registerTimelineRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/timeline", s.handleTimeline)
}

// handleTimeline reads the log once and projects each event onto its envelope
// fields (n, ts, type, actor, task?, feature?). Read-only; no engine, no writes.
func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	evs, err := event.ReadAll(logPath(dir))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := TimelineDTO{Total: len(evs), Events: make([]TimelineEventDTO, 0, len(evs))}
	for i, e := range evs {
		out.Events = append(out.Events, TimelineEventDTO{
			N:       i + 1,
			TS:      e.TS,
			Type:    e.EventType,
			Actor:   e.AgentID,
			Task:    e.TaskID,
			Feature: e.Feature,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// parseAt reads the optional `at` query param. ok=false with present=true means
// the value was malformed/negative (caller should 400). present=false means the
// param was absent (caller should serve the full state unchanged). When present
// and valid, returns the non-negative at value.
func parseAt(r *http.Request) (at int, present, ok bool) {
	raw := r.URL.Query().Get("at")
	if raw == "" {
		return 0, false, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, true, false
	}
	return n, true, true
}
