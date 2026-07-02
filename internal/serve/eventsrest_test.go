package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

// eventsLogServer seeds a project at root and serves it as "pactify".
func eventsLogServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(New([]registry.Project{{Name: "pactify", Path: root}}).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func getEventsLog(t *testing.T, url string) (int, []map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	var out []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return resp.StatusCode, out
}

// seedEventLines writes .pact/log.jsonl with n synthetic checkpoint events
// (event_id "e0".."e<n-1>") after an init line, and returns total line count.
func seedEventLines(t *testing.T, root string, n int) int {
	t.Helper()
	seedProject(t, root, "pactify")
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `{"event_id":"e%d","ts":"2026-01-01T00:00:00Z","agent_id":"claude-opus","role":"worker","event_type":"checkpoint","task_id":"T1","feature":"F","payload":{}}`+"\n", i)
	}
	f, err := os.OpenFile(filepath.Join(root, ".pact", "log.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(b.String()); err != nil {
		t.Fatalf("write log: %v", err)
	}
	return n + 1 // + the seed init line
}

// TestEventsLogTailShape: full tail in file order, raw-event shape (the same
// objects the SSE `pact` frames carry), and the n param trims to the newest n.
func TestEventsLogTailShape(t *testing.T) {
	root := t.TempDir()
	total := seedEventLines(t, root, 3)
	ts := eventsLogServer(t, root)

	code, evs := getEventsLog(t, ts.URL+"/api/projects/pactify/events/log")
	if code != http.StatusOK || len(evs) != total {
		t.Fatalf("want 200 with %d events, got %d with %d", total, code, len(evs))
	}
	if evs[0]["event_type"] != "init" || evs[total-1]["event_id"] != "e2" {
		t.Fatalf("wrong order/shape: first=%+v last=%+v", evs[0], evs[total-1])
	}
	// raw envelope fields present, as the SSE stream sends them
	for _, k := range []string{"event_id", "ts", "agent_id", "event_type", "payload"} {
		if _, ok := evs[1][k]; !ok {
			t.Fatalf("event missing %q: %+v", k, evs[1])
		}
	}

	code, evs = getEventsLog(t, ts.URL+"/api/projects/pactify/events/log?n=2")
	if code != http.StatusOK || len(evs) != 2 {
		t.Fatalf("n=2: want 2 events, got %d (code %d)", len(evs), code)
	}
	if evs[0]["event_id"] != "e1" || evs[1]["event_id"] != "e2" {
		t.Fatalf("n=2 must be the newest two: %+v", evs)
	}
}

// TestEventsLogDefaultAndCap: no n → 500 newest; n above the cap → 2000.
func TestEventsLogDefaultAndCap(t *testing.T) {
	root := t.TempDir()
	seedEventLines(t, root, 2100)
	ts := eventsLogServer(t, root)

	code, evs := getEventsLog(t, ts.URL+"/api/projects/pactify/events/log")
	if code != http.StatusOK || len(evs) != eventsLogDefaultN {
		t.Fatalf("default: want %d events, got %d (code %d)", eventsLogDefaultN, len(evs), code)
	}
	if evs[len(evs)-1]["event_id"] != "e2099" {
		t.Fatalf("default tail must end at newest: %+v", evs[len(evs)-1])
	}

	code, evs = getEventsLog(t, ts.URL+"/api/projects/pactify/events/log?n=99999")
	if code != http.StatusOK || len(evs) != eventsLogMaxN {
		t.Fatalf("cap: want %d events, got %d (code %d)", eventsLogMaxN, len(evs), code)
	}
}

// TestEventsLogBadParams: malformed/negative n → 400; unknown project → 404.
func TestEventsLogBadParams(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "pactify")
	ts := eventsLogServer(t, root)

	for _, q := range []string{"?n=abc", "?n=-1"} {
		if code, _ := getEventsLog(t, ts.URL+"/api/projects/pactify/events/log"+q); code != http.StatusBadRequest {
			t.Fatalf("%s: want 400 got %d", q, code)
		}
	}
	if code, _ := getEventsLog(t, ts.URL+"/api/projects/nope/events/log"); code != http.StatusNotFound {
		t.Fatalf("unknown project: want 404 got %d", code)
	}
}

// TestEventsLogWorktree: wt addresses that worktree's ledger; an unknown wt is
// a 400 (mirrors handleState).
func TestEventsLogWorktree(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "pactify")
	wtRoot := t.TempDir()
	seedEventLines(t, wtRoot, 1) // wt ledger: init + e0

	orig := execGitWorktree
	defer func() { execGitWorktree = orig }()
	execGitWorktree = func(string) ([]byte, error) {
		return []byte("worktree " + root + "\nbranch refs/heads/main\n\n" +
			"worktree " + wtRoot + "\nbranch refs/heads/feat-x\n\n"), nil
	}
	ts := eventsLogServer(t, root)

	code, evs := getEventsLog(t, ts.URL+"/api/projects/pactify/events/log?wt=feat-x")
	if code != http.StatusOK || len(evs) != 2 {
		t.Fatalf("wt ledger: want 2 events, got %d (code %d)", len(evs), code)
	}
	if evs[1]["event_id"] != "e0" {
		t.Fatalf("wt ledger tail: %+v", evs)
	}
	if code, _ := getEventsLog(t, ts.URL+"/api/projects/pactify/events/log?wt=nope"); code != http.StatusBadRequest {
		t.Fatalf("unknown wt: want 400 got %d", code)
	}
}
