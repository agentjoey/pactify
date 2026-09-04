package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/orchestrate"
	"github.com/agentjoey/pactify/internal/registry"
)

// writeScopedProposal drops the sidecar an env-class escalation leaves for one
// scope (a feature id, or "all" for an unfiltered serial run).
func writeScopedProposal(t *testing.T, dir, scope, task, seat string) {
	t.Helper()
	p := filepath.Join(dir, ".pact", "orchestrate", "fallback")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"task":"` + task + `","seat":"` + seat + `","fromRole":"primary","toRole":"backup",` +
		`"reason":"worker run: run timeout (--run-timeout) exceeded","tried":["backup"]}`
	if err := os.WriteFile(filepath.Join(p, scope+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

type proposalsResp struct {
	Proposals []struct {
		Scope    string `json:"scope"`
		Task     string `json:"task"`
		Seat     string `json:"seat"`
		FromRole string `json:"fromRole"`
		ToRole   string `json:"toRole"`
		Reason   string `json:"reason"`
	} `json:"proposals"`
}

func getProposals(t *testing.T, url string) proposalsResp {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET proposals status=%d", resp.StatusCode)
	}
	var out proposalsResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func approve(t *testing.T, url, task string) *http.Response {
	t.Helper()
	body := []byte(`{"task":"` + task + `"}`)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// GET aggregates every pending proposal, sorted by scope, so the dashboard can
// render one card per human decision.
func TestFallbackProposalsAggregate(t *testing.T) {
	dir := seedOrchRepo(t)
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	url := ts.URL + "/api/projects/p/fallback-proposal"

	// 0: no directory at all → empty list, never an error.
	if got := getProposals(t, url); len(got.Proposals) != 0 {
		t.Fatalf("no proposals → empty list, got %+v", got)
	}

	// 1
	writeScopedProposal(t, dir, "fb", "tb", "w")
	got := getProposals(t, url)
	if len(got.Proposals) != 1 || got.Proposals[0].Task != "tb" || got.Proposals[0].Scope != "fb" {
		t.Fatalf("one proposal not surfaced: %+v", got)
	}
	if got.Proposals[0].Seat != "w" || got.Proposals[0].FromRole != "primary" || got.Proposals[0].ToRole != "backup" {
		t.Fatalf("card needs seat and both roles: %+v", got.Proposals[0])
	}
	if !strings.Contains(got.Proposals[0].Reason, "run timeout") {
		t.Fatalf("card needs the failure reason: %+v", got.Proposals[0])
	}

	// N, sorted by scope
	writeScopedProposal(t, dir, "fa", "ta", "w")
	writeScopedProposal(t, dir, "fc", "tc", "w2")
	got = getProposals(t, url)
	if len(got.Proposals) != 3 {
		t.Fatalf("want 3 proposals, got %+v", got)
	}
	for i, want := range []string{"fa", "fb", "fc"} {
		if got.Proposals[i].Scope != want {
			t.Fatalf("proposals must be sorted by scope: %+v", got)
		}
	}
}

// A half-written or unparseable sidecar is skipped, not a 500 — and a proposal
// missing a required field must never become a live approve button.
func TestFallbackProposalsSkipUnreadable(t *testing.T) {
	dir := seedOrchRepo(t)
	fb := filepath.Join(dir, ".pact", "orchestrate", "fallback")
	if err := os.MkdirAll(fb, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"half.json":   `{"task":"t","seat":"w","toRo`,
		"noseat.json": `{"task":"t","toRole":"backup"}`,
		"notgt.json":  `{"task":"t","seat":"w"}`,
		"empty.json":  `{}`,
	} {
		if err := os.WriteFile(filepath.Join(fb, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeScopedProposal(t, dir, "good", "tgood", "w")
	// A non-JSON file in the directory must be ignored outright.
	if err := os.WriteFile(filepath.Join(fb, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := New([]registry.Project{{Name: "p", Path: dir}})
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	got := getProposals(t, ts.URL+"/api/projects/p/fallback-proposal")
	if len(got.Proposals) != 1 || got.Proposals[0].Task != "tgood" {
		t.Fatalf("only the readable proposal may surface: %+v", got)
	}
}

// The pre-FALLBACK-PAR single-file path is no longer read.
func TestFallbackProposalsIgnoreLegacySingleFile(t *testing.T) {
	dir := seedOrchRepo(t)
	p := filepath.Join(dir, ".pact", "orchestrate")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"task":"old","seat":"w","fromRole":"primary","toRole":"backup"}`
	if err := os.WriteFile(filepath.Join(p, "fallback-proposal.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	if got := getProposals(t, ts.URL+"/api/projects/p/fallback-proposal"); len(got.Proposals) != 0 {
		t.Fatalf("the legacy path must not be read: %+v", got)
	}
}

// POST approve resumes the run WITH --approve-fallback <task>: the task id is
// what makes the driver adopt exactly the proposal the operator clicked.
func TestFallbackApproveSpawnsResumeWithTask(t *testing.T) {
	dir := seedOrchRepo(t)
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	writeScopedProposal(t, dir, "fa", "ta", "w")
	writeScopedProposal(t, dir, "fb", "tb", "w")

	ran := make(chan []string, 1)
	srv.execOrchestrate = func(_ string, args, _ []string) error { ran <- args; return nil }

	resp := approve(t, ts.URL+"/api/projects/p/fallback-proposal/approve", "tb")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("approve should 202, got %d", resp.StatusCode)
	}
	select {
	case args := <-ran:
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--approve-fallback tb") {
			t.Fatalf("approve must name the task, got %v", args)
		}
		if strings.Contains(joined, "ta") {
			t.Fatalf("approve must not adopt the other feature's proposal, got %v", args)
		}
		if !strings.Contains(joined, "--resume") {
			t.Fatalf("approve must resume the paused run, got %v", args)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approve did not spawn orchestrate")
	}
}

// Approving a task with nothing pending is a 404 and spawns nothing: a stale
// card must not restart a run.
func TestFallbackApproveUnknownTask(t *testing.T) {
	dir := seedOrchRepo(t)
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	writeScopedProposal(t, dir, "fa", "ta", "w")
	srv.execOrchestrate = func(_ string, _, _ []string) error {
		t.Fatal("must not spawn orchestrate for a task with no pending proposal")
		return nil
	}
	for _, task := range []string{"nope", ""} {
		resp := approve(t, ts.URL+"/api/projects/p/fallback-proposal/approve", task)
		code := resp.StatusCode
		resp.Body.Close()
		if code != http.StatusNotFound {
			t.Fatalf("approve %q should 404, got %d", task, code)
		}
	}
}

// A parallel run's approval must resume AT THE SAME CONCURRENCY. serve has no
// memory of the run's shape, so it reads back the driver's run-params file;
// without it a --max-concurrency 3 run silently becomes serial (§2.8).
func TestFallbackApproveReplaysRunConcurrency(t *testing.T) {
	dir := seedOrchRepo(t)
	if err := orchestrate.WriteRunParams(dir, orchestrate.RunParams{MaxConcurrency: 3}); err != nil {
		t.Fatal(err)
	}
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	writeScopedProposal(t, dir, "fa", "ta", "w")

	ran := make(chan []string, 1)
	srv.execOrchestrate = func(_ string, args, _ []string) error { ran <- args; return nil }
	resp := approve(t, ts.URL+"/api/projects/p/fallback-proposal/approve", "ta")
	defer resp.Body.Close()
	select {
	case args := <-ran:
		if !strings.Contains(strings.Join(args, " "), "--max-concurrency 3") {
			t.Fatalf("approve must resume at the run's concurrency, got %v", args)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approve did not spawn orchestrate")
	}
}

// Missing or corrupt run-params → serial, no error: fail-safe, and exactly what
// this path did before run-params existed.
func TestFallbackApproveWithoutRunParamsIsSerial(t *testing.T) {
	for name, seed := range map[string]func(dir string){
		"missing": func(string) {},
		"invalid": func(dir string) {
			p := filepath.Join(dir, ".pact", "orchestrate")
			os.MkdirAll(p, 0o755)
			os.WriteFile(filepath.Join(p, "run-params.json"), []byte("{oops"), 0o644)
		},
		"serial": func(dir string) {
			_ = orchestrate.WriteRunParams(dir, orchestrate.RunParams{MaxConcurrency: 1})
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := seedOrchRepo(t)
			seed(dir)
			srv := New([]registry.Project{{Name: "p", Path: dir}})
			srv.SetSeat("claude")
			ts := httptest.NewServer(srv.Handler())
			defer ts.Close()
			writeScopedProposal(t, dir, "fa", "ta", "w")

			ran := make(chan []string, 1)
			srv.execOrchestrate = func(_ string, args, _ []string) error { ran <- args; return nil }
			resp := approve(t, ts.URL+"/api/projects/p/fallback-proposal/approve", "ta")
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("approve should 202, got %d", resp.StatusCode)
			}
			select {
			case args := <-ran:
				if strings.Contains(strings.Join(args, " "), "--max-concurrency") {
					t.Fatalf("%s run-params must resume serially, got %v", name, args)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("approve did not spawn orchestrate")
			}
		})
	}
}

// run-params.json must not be mistaken for a feature by the parallel aggregator —
// which is exactly why it does not live under .pact/orchestrate/parallel/.
func TestRunParamsDoNotPolluteParallelAggregation(t *testing.T) {
	dir := seedOrchRepo(t)
	if err := orchestrate.WriteRunParams(dir, orchestrate.RunParams{MaxConcurrency: 3}); err != nil {
		t.Fatal(err)
	}
	pd := filepath.Join(dir, ".pact", "orchestrate", "parallel")
	if err := os.MkdirAll(pd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pd, "fa.json"), []byte(`{"feature":"fa"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/projects/p/orchestrate/parallel")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out ParallelStatusDTO
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Features) != 1 {
		t.Fatalf("run-params must not show up as a feature: %d features", len(out.Features))
	}
}

// An unknown project is a 404 on both verbs — never a silent 200 with an empty
// list, which would look identical to "this project has nothing pending".
func TestFallbackProposalUnknownProject(t *testing.T) {
	srv := New([]registry.Project{{Name: "p", Path: seedOrchRepo(t)}})
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	get, err := http.Get(ts.URL + "/api/projects/nope/fallback-proposal")
	if err != nil {
		t.Fatal(err)
	}
	get.Body.Close()
	if get.StatusCode != http.StatusNotFound {
		t.Fatalf("GET unknown project should 404, got %d", get.StatusCode)
	}
	post := approve(t, ts.URL+"/api/projects/nope/fallback-proposal/approve", "ta")
	post.Body.Close()
	if post.StatusCode != http.StatusNotFound {
		t.Fatalf("POST unknown project should 404, got %d", post.StatusCode)
	}
}

// A rejected approval (a run already in flight) must leave the proposal intact:
// the run is still paused and the operator must be able to retry.
func TestFallbackApproveConflictKeepsProposal(t *testing.T) {
	dir := seedOrchRepo(t)
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	srv.SetSeat("claude")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	writeScopedProposal(t, dir, "fa", "ta", "w")

	if !orchMarkRunning(dir) {
		t.Fatal("could not claim the run marker")
	}
	defer orchClearRunning(dir)

	resp := approve(t, ts.URL+"/api/projects/p/fallback-proposal/approve", "ta")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("approve during a live run should 409, got %d", resp.StatusCode)
	}
	if len(readFallbackProposals(dir)) != 1 {
		t.Fatal("a refused approval must not consume the proposal")
	}
}
