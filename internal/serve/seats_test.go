package serve

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// getSeats fetches /api/projects/{id}/seats and decodes the provenance rows.
func getSeats(t *testing.T, baseURL, id string) []seatProvenanceDTO {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/projects/" + id + "/seats")
	if err != nil {
		t.Fatalf("GET seats: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seats: want 200 got %d", resp.StatusCode)
	}
	var out []seatProvenanceDTO
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode seats: %v", err)
	}
	return out
}

func findSeat(t *testing.T, rows []seatProvenanceDTO, id string) seatProvenanceDTO {
	t.Helper()
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("seat %q not in rows: %+v", id, rows)
	return seatProvenanceDTO{}
}

// TestSeatsNeverJoined: a roster seat with no join event has no lastJoin and
// clientChanged false.
func TestSeatsNeverJoined(t *testing.T) {
	dir := newAuthorRepo(t)
	ts := authorServer(t, dir, "")
	rows := getSeats(t, ts.URL, "pactify")
	// roster has both seats; neither has joined.
	if len(rows) != 2 {
		t.Fatalf("want 2 roster seats got %d: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.LastJoin != nil {
			t.Fatalf("seat %q never joined but has lastJoin: %+v", r.ID, r.LastJoin)
		}
		if r.ClientChanged {
			t.Fatalf("seat %q clientChanged must be false: %+v", r.ID, r)
		}
	}
}

// TestSeatsSingleCLIJoin: a plain Join (CLI wrapper) surfaces pactify-cli as the
// last join client; clientChanged false (only one join).
func TestSeatsSingleCLIJoin(t *testing.T) {
	dir := newAuthorRepo(t)
	old := pact.ClientVersion
	pact.ClientVersion = "1.0.0"
	t.Cleanup(func() { pact.ClientVersion = old })
	if err := pact.At(dir).As("opencode").Join("opencode", "worker"); err != nil {
		t.Fatalf("join: %v", err)
	}
	ts := authorServer(t, dir, "")
	row := findSeat(t, getSeats(t, ts.URL, "pactify"), "opencode")
	if row.LastJoin == nil {
		t.Fatalf("opencode joined but lastJoin nil: %+v", row)
	}
	if row.LastJoin.Client != "pactify-cli" || row.LastJoin.Version != "1.0.0" {
		t.Fatalf("lastJoin client = %+v want pactify-cli/1.0.0", row.LastJoin)
	}
	if row.LastJoin.TS == "" {
		t.Fatalf("lastJoin ts should be set: %+v", row.LastJoin)
	}
	if row.ClientChanged {
		t.Fatalf("single join must not flag clientChanged: %+v", row)
	}
}

// TestSeatsClientChanged: two joins by the SAME seat with DIFFERENT client names
// flag clientChanged true, and lastJoin reflects the most recent client.
func TestSeatsClientChanged(t *testing.T) {
	dir := newAuthorRepo(t)
	w := pact.At(dir).As("opencode")
	if err := w.JoinWithClient("opencode", "worker", "opencode", "0.1"); err != nil {
		t.Fatalf("join1: %v", err)
	}
	if err := w.JoinWithClient("opencode", "worker", "claude-code", "0.2"); err != nil {
		t.Fatalf("join2: %v", err)
	}
	ts := authorServer(t, dir, "")
	row := findSeat(t, getSeats(t, ts.URL, "pactify"), "opencode")
	if !row.ClientChanged {
		t.Fatalf("differing client names must flag clientChanged: %+v", row)
	}
	if row.LastJoin == nil || row.LastJoin.Client != "claude-code" {
		t.Fatalf("lastJoin should be the most recent client: %+v", row.LastJoin)
	}
	// prevClient carries the FIRST (differing) join's client name for a before→after hint.
	if row.LastJoin.PrevClient != "opencode" {
		t.Fatalf("prevClient should be the previous join's client: %+v", row.LastJoin)
	}
}

// TestSeatsClientUnchanged: two joins with the SAME client name do not flag
// clientChanged.
func TestSeatsClientUnchanged(t *testing.T) {
	dir := newAuthorRepo(t)
	w := pact.At(dir).As("opencode")
	if err := w.JoinWithClient("opencode", "worker", "opencode", "0.1"); err != nil {
		t.Fatalf("join1: %v", err)
	}
	if err := w.JoinWithClient("opencode", "worker", "opencode", "0.2"); err != nil {
		t.Fatalf("join2: %v", err)
	}
	ts := authorServer(t, dir, "")
	row := findSeat(t, getSeats(t, ts.URL, "pactify"), "opencode")
	if row.ClientChanged {
		t.Fatalf("same client name twice must not flag clientChanged: %+v", row)
	}
}
