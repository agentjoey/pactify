package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

// writePendingProposal drops the sidecar an env-class escalation would leave.
func writePendingProposal(t *testing.T, dir string) {
	t.Helper()
	p := filepath.Join(dir, ".pact", "orchestrate")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"task":"t1","seat":"w","fromRole":"primary","toRole":"backup","reason":"worker run: run timeout (--run-timeout) exceeded","tried":["backup"]}`
	if err := os.WriteFile(filepath.Join(p, "fallback-proposal.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// GET surfaces the pending proposal so the dashboard can render the card.
func TestFallbackProposalGET(t *testing.T) {
	dir := seedOrchRepo(t)
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// No proposal yet → pending:false, and the card must not render.
	resp, err := http.Get(ts.URL + "/api/projects/p/fallback-proposal")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Pending  bool   `json:"pending"`
		Seat     string `json:"seat"`
		FromRole string `json:"fromRole"`
		ToRole   string `json:"toRole"`
		Reason   string `json:"reason"`
		Task     string `json:"task"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Pending {
		t.Fatalf("no sidecar → pending must be false, got %+v", out)
	}

	writePendingProposal(t, dir)
	resp2, err := http.Get(ts.URL + "/api/projects/p/fallback-proposal")
	if err != nil {
		t.Fatal(err)
	}
	json.NewDecoder(resp2.Body).Decode(&out)
	resp2.Body.Close()
	if !out.Pending || out.Seat != "w" || out.FromRole != "primary" || out.ToRole != "backup" {
		t.Fatalf("proposal not surfaced: %+v", out)
	}
	if !strings.Contains(out.Reason, "run timeout") || out.Task != "t1" {
		t.Fatalf("card needs the reason and task: %+v", out)
	}
}

// POST approve resumes the run WITH --approve-fallback, which is what makes the
// driver adopt the proposal for that run.
func TestFallbackProposalApproveSpawnsResumeWithFlag(t *testing.T) {
	dir := seedOrchRepo(t)
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	writePendingProposal(t, dir)

	ran := make(chan []string, 1)
	srv.execOrchestrate = func(_ string, args, _ []string) error { ran <- args; return nil }

	resp, err := http.Post(ts.URL+"/api/projects/p/fallback-proposal/approve", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("approve should 202, got %d", resp.StatusCode)
	}
	select {
	case args := <-ran:
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--approve-fallback") {
			t.Fatalf("approve must pass --approve-fallback, got %v", args)
		}
		if !strings.Contains(joined, "--resume") {
			t.Fatalf("approve must resume the paused run, got %v", args)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approve did not spawn orchestrate")
	}
}

// Approving with nothing pending is a 404, not a silent resume — the operator
// must not accidentally restart a run by clicking a stale card.
func TestFallbackProposalApproveWithoutPending(t *testing.T) {
	dir := seedOrchRepo(t)
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	srv.execOrchestrate = func(_ string, _, _ []string) error {
		t.Fatal("must not spawn orchestrate when no proposal is pending")
		return nil
	}
	resp, err := http.Post(ts.URL+"/api/projects/p/fallback-proposal/approve", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("approve with no pending proposal should 404, got %d", resp.StatusCode)
	}
}
