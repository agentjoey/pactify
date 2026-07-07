package projection

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/event"
)

// A seat that joins/inits with no roles must project to roles: [] (a non-nil
// slice), never null. A null crashes null-unsafe clients (the dashboard Board
// does a.roles.length → blank page for that project). Found on the local
// pactify project (seats oc-finish/oc-sessions had roles: null).
func TestSeatRolesNeverNull(t *testing.T) {
	evs := []event.Event{
		{EventType: "init", AgentID: "claude", Payload: map[string]any{
			"project": "p",
			"seats": []any{
				map[string]any{"id": "claude", "roles": []any{"orchestrator"}},
				map[string]any{"id": "norole"}, // no roles key → would be nil
			},
		}},
	}
	st := Project(evs)
	var norole *Seat
	for i := range st.Agents {
		if st.Agents[i].ID == "norole" {
			norole = &st.Agents[i]
		}
	}
	if norole == nil {
		t.Fatal("seat 'norole' missing from projection")
	}
	if norole.Roles == nil {
		t.Fatal("empty-role seat has nil Roles — will marshal to null and crash the dashboard")
	}
	// And it must marshal as [] not null.
	b, _ := json.Marshal(st)
	if strings.Contains(string(b), `"Roles":null`) || strings.Contains(string(b), `"roles":null`) {
		t.Fatalf("state JSON contains a null roles field:\n%s", b)
	}
}
