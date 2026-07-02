package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

func seedGuardRepo(t *testing.T, baseBranch string) string {
	t.Helper()
	root := t.TempDir()
	pactDir := filepath.Join(root, ".pact")
	os.MkdirAll(filepath.Join(pactDir, "tasks"), 0o755)
	initLine := `{"event_id":"1","ts":"2026-01-01T00:00:00Z","agent_id":"claude","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"testp","protocol_version":1,"seats":[{"id":"claude","roles":["orchestrator","reviewer"],"entry":"CLAUDE.md"}],"base_branch":"` + baseBranch + `"}}` + "\n"
	os.WriteFile(filepath.Join(pactDir, "log.jsonl"), []byte(initLine), 0o644)
	return root
}

// Two rapid POSTs to /orchestrate/run must never spawn two drivers: the child
// takes seconds before its first status.json write (and none exists at all on a
// first run), so only the in-memory marker can close the check→spawn window.
func TestRunDoublePostSecondConflict(t *testing.T) {
	root := seedGuardRepo(t, "main")

	entered := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	srv.SetExecOrchestrate(func(dir string, args, env []string) error {
		calls++
		if calls == 1 {
			close(entered)
			<-release
		}
		return nil
	})
	ts := newTestServer(t, srv)

	firstDone := make(chan int)
	go func() {
		resp := postJSON(t, ts.URL+"/api/projects/p/orchestrate/run", map[string]any{"feature": "f1"})
		defer resp.Body.Close()
		firstDone <- resp.StatusCode
	}()

	<-entered // first request is inside the (still running) exec
	resp := postJSON(t, ts.URL+"/api/projects/p/orchestrate/run", map[string]any{"feature": "f1"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second POST status=%d, want 409", resp.StatusCode)
	}

	close(release)
	if code := <-firstDone; code != http.StatusAccepted {
		t.Fatalf("first POST status=%d, want 202", code)
	}
	if calls != 1 {
		t.Fatalf("exec called %d times, want 1", calls)
	}

	// The injected exec has returned, so the marker must be cleared and a
	// fresh run allowed.
	resp2 := postJSON(t, ts.URL+"/api/projects/p/orchestrate/run", map[string]any{"feature": "f1"})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("third POST status=%d, want 202", resp2.StatusCode)
	}
}

// A failed spawn must release the marker, or the project is bricked until
// serve restarts.
func TestRunExecErrorClearsMarker(t *testing.T) {
	root := seedGuardRepo(t, "main")

	fail := true
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	srv.SetExecOrchestrate(func(dir string, args, env []string) error {
		if fail {
			return os.ErrNotExist
		}
		return nil
	})
	ts := newTestServer(t, srv)

	resp := postJSON(t, ts.URL+"/api/projects/p/orchestrate/run", map[string]any{"feature": "f1"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", resp.StatusCode)
	}

	fail = false
	resp2 := postJSON(t, ts.URL+"/api/projects/p/orchestrate/run", map[string]any{"feature": "f1"})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("retry status=%d, want 202", resp2.StatusCode)
	}
}

// Ship with no branch must target the ledger's base branch, not literal "main":
// the dashboard's shipFeature never sends one.
func TestShipDefaultsToLedgerBase(t *testing.T) {
	root := seedGuardRepo(t, "develop")

	var got []string
	srv := New([]registry.Project{{Name: "p", Path: root}})
	srv.SetSeat("claude")
	srv.SetFinishRunner(func(dir, name string, args ...string) (string, error) {
		got = append([]string{name}, args...)
		return "pushed\n", nil
	})
	ts := newTestServer(t, srv)

	resp := postJSON(t, ts.URL+"/api/projects/p/orchestrate/ship", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("status=%d body=%v, want 200", resp.StatusCode, body)
	}
	if want := []string{"git", "push", "origin", "develop"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("finish runner args=%v, want %v", got, want)
	}
}

// A rebaseline event overrides the init-time base, same as internal/pact.
func TestShipDefaultsToRebaselineBase(t *testing.T) {
	root := seedGuardRepo(t, "develop")
	rebase := `{"event_id":"2","ts":"2026-01-02T00:00:00Z","agent_id":"claude","role":"orchestrator","event_type":"rebaseline","task_id":"","feature":"","payload":{"base_branch":"release"}}` + "\n"
	f, err := os.OpenFile(filepath.Join(root, ".pact", "log.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(rebase)
	f.Close()

	if got := shipBaseBranch(root); got != "release" {
		t.Fatalf("shipBaseBranch=%q, want %q", got, "release")
	}
}
