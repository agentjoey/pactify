package serve

import (
	"net/http"

	"github.com/agentjoey/pactify/internal/event"
)

// registerSeatsRoutes wires the seats provenance endpoint onto mux.
func (s *Server) registerSeatsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/seats", s.handleSeats)
}

// lastJoinDTO is the most recent join's self-reported client metadata. Omitted
// entirely when the seat's latest join carried no client object.
type lastJoinDTO struct {
	Client  string `json:"client,omitempty"`
	Version string `json:"version,omitempty"`
	TS      string `json:"ts"`
}

type seatProvenanceDTO struct {
	ID            string       `json:"id"`
	Roles         []string     `json:"roles"`
	LastJoin      *lastJoinDTO `json:"lastJoin,omitempty"`
	ClientChanged bool         `json:"clientChanged"`
}

// handleSeats folds the roster (from folded state) with join provenance (from
// the raw log): each seat's most recent join client+version+ts, and a
// clientChanged flag when the two most recent joins both name a client and the
// names differ.
func (s *Server) handleSeats(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	dto, err := ProjectState(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	evs, err := event.ReadAll(logPath(dir))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Group join events per seat in append (file) order.
	joinsBySeat := map[string][]event.Event{}
	for _, ev := range evs {
		if ev.EventType == "join" {
			joinsBySeat[ev.AgentID] = append(joinsBySeat[ev.AgentID], ev)
		}
	}

	out := []seatProvenanceDTO{}
	for _, a := range dto.Agents {
		row := seatProvenanceDTO{ID: a.ID, Roles: a.Roles}
		joins := joinsBySeat[a.ID]
		if n := len(joins); n > 0 {
			name, version := joinClient(joins[n-1])
			row.LastJoin = &lastJoinDTO{Client: name, Version: version, TS: joins[n-1].TS}
			if n >= 2 {
				prevName, _ := joinClient(joins[n-2])
				if name != "" && prevName != "" && name != prevName {
					row.ClientChanged = true
				}
			}
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// joinClient extracts the self-reported client name+version from a join event's
// payload. Missing/malformed client object yields empty strings.
func joinClient(ev event.Event) (name, version string) {
	cl, ok := ev.Payload["client"].(map[string]any)
	if !ok {
		return "", ""
	}
	name, _ = cl["name"].(string)
	version, _ = cl["version"].(string)
	return name, version
}
