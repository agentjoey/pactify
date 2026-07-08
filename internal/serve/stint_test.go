package serve

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/orchestrate"
	"github.com/agentjoey/pactify/internal/registry"
	"github.com/agentjoey/pactify/internal/remoteexec"
)

type fakeRunner struct{ ran chan orchestrate.LaunchContext }

func (f *fakeRunner) Run(_ context.Context, lc orchestrate.LaunchContext) error {
	f.ran <- lc
	return nil
}

func writePolicy(t *testing.T, dir, json string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".pact"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pact", "remote.json"), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newStinterFor(dir string) (*serveStinter, *fakeRunner) {
	srv := New([]registry.Project{{Name: "demo", Path: dir}})
	fr := &fakeRunner{ran: make(chan orchestrate.LaunchContext, 1)}
	return &serveStinter{s: srv, runner: fr}, fr
}

func TestServeStinter_PolicyGate(t *testing.T) {
	dir := t.TempDir()
	st, fr := newStinterFor(dir)
	req := remoteexec.StintRequest{Project: "demo", Task: "t1", Seat: "kimi-worker", AgentKind: "kimi-cli", Briefing: "go"}

	// No policy file → denied.
	if err := st.RunStint(req); err == nil {
		t.Fatal("stint with no policy should be denied")
	}

	// Policy stint=true → accepted; no git remote → runs in the project tree.
	writePolicy(t, dir, `{"stint":true}`)
	if err := st.RunStint(req); err != nil {
		t.Fatalf("stint should be allowed: %v", err)
	}
	select {
	case lc := <-fr.ran:
		if lc.Task != "t1" || lc.Seat != "kimi-worker" || lc.Kind != "kimi-cli" || lc.RepoDir != dir {
			t.Fatalf("launch context wrong: %+v", lc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner was not spawned")
	}
}

func TestServeStinter_AgentKindAllowlist(t *testing.T) {
	dir := t.TempDir()
	st, _ := newStinterFor(dir)
	writePolicy(t, dir, `{"stint":true,"agentKinds":["opencode"]}`)
	if err := st.RunStint(remoteexec.StintRequest{Project: "demo", Task: "t", Seat: "s", AgentKind: "kimi-cli"}); err == nil {
		t.Fatal("agent kind outside allowlist should be denied")
	}
}

func TestServeStinter_UnknownProject(t *testing.T) {
	st, _ := newStinterFor(t.TempDir())
	if err := st.RunStint(remoteexec.StintRequest{Project: "nope", Task: "t", Seat: "s", AgentKind: "k"}); err == nil {
		t.Fatal("unknown project should be denied")
	}
}

func TestServeStinter_UsesDefaultTransportModes(t *testing.T) {
	dir := t.TempDir()
	srv := New([]registry.Project{{Name: "demo", Path: dir}})
	st, ok := srv.newStinter().(*serveStinter)
	if !ok {
		t.Fatalf("newStinter should return *serveStinter, got %T", srv.newStinter())
	}
	rr, ok := st.runner.(orchestrate.RoutedLocalRunner)
	if !ok {
		t.Fatalf("newStinter runner should be RoutedLocalRunner, got %T", st.runner)
	}
	if rr.Modes["opencode"] != "acp" {
		t.Fatalf("opencode should default to acp, modes=%v", rr.Modes)
	}
	if rr.Cmd == nil || rr.Acp == nil {
		t.Fatalf("RoutedLocalRunner must have both Cmd and Acp runners set")
	}
}

// git runs a git command in dir (test helper).
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestServeStinter_RemoteLifecycle proves the branch-as-interface flow on real
// git: a "driver" repo pushes an assign (feature branch + ledger) to a bare
// origin; the "worker machine" repo (this stinter's project) fetches, runs the
// agent in an isolated worktree on that branch, and pushes back. The driver then
// sees the stint's commit on origin — the exact signal the M3 RemoteRunner polls.
func TestServeStinter_RemoteLifecycle(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	driver := filepath.Join(root, "driver")
	workerRepo := filepath.Join(root, "worker")
	gitRun(t, root, "init", "--bare", origin)
	gitRun(t, root, "clone", origin, driver)

	// Driver: seed main + a feature branch with the pact ledger (assign for t1).
	gitRun(t, driver, "commit", "--allow-empty", "-m", "init")
	gitRun(t, driver, "push", "origin", "HEAD:main")
	gitRun(t, driver, "checkout", "-b", "feat-f1")
	if err := os.MkdirAll(filepath.Join(driver, ".pact"), 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := `{"event_id":"e1","ts":"2026-01-01T00:00:00Z","agent_id":"o","role":"orchestrator","event_type":"init","task_id":"","feature":"","payload":{"project":"demo","base_branch":"main","protocol_version":1,"seats":[{"id":"o","roles":["orchestrator"],"entry":"x"},{"id":"w","roles":["worker"],"entry":"x"}]}}
{"event_id":"e2","ts":"2026-01-01T00:01:00Z","agent_id":"o","role":"orchestrator","event_type":"assign","task_id":"t1","feature":"f1","payload":{"branch":"feat-f1","owner":"w","reviewer":"o","spec":"s.md"}}
`
	if err := os.WriteFile(filepath.Join(driver, ".pact", "log.jsonl"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, driver, "add", ".pact")
	gitRun(t, driver, "commit", "-m", "assign t1")
	gitRun(t, driver, "push", "origin", "feat-f1")

	// Worker machine: a clone of the same origin, on main (not the feature).
	gitRun(t, root, "clone", origin, workerRepo)
	gitRun(t, workerRepo, "checkout", "main")

	// The "agent": drops a file + commits, in whatever RepoDir the stint gives it.
	fr := &fakeRunner{ran: make(chan orchestrate.LaunchContext, 1)}
	agent := &lifecycleRunner{inner: fr, t: t}
	srv := New([]registry.Project{{Name: "demo", Path: workerRepo}})
	st := &serveStinter{s: srv, runner: agent}
	writePolicy(t, workerRepo, `{"stint":true}`)

	if err := st.RunStint(remoteexec.StintRequest{Project: "demo", Task: "t1", Seat: "w", AgentKind: "kimi-cli", Branch: "feat-f1"}); err != nil {
		t.Fatalf("stint rejected: %v", err)
	}
	// The agent must have run in an ISOLATED WORKTREE on feat-f1 (not the repo tree).
	var lc orchestrate.LaunchContext
	select {
	case lc = <-fr.ran:
	case <-time.After(5 * time.Second):
		t.Fatal("agent never ran")
	}
	if lc.RepoDir == workerRepo {
		t.Fatalf("stint should run in an isolated worktree, ran in the project tree")
	}

	// Driver side: poll origin for the stint's pushed commit (the M3 completion
	// signal). The lifecycle pushes after the agent returns.
	deadline := time.Now().Add(5 * time.Second)
	for {
		gitRun(t, driver, "fetch", "origin", "feat-f1")
		out, err := exec.Command("git", "-C", driver, "log", "origin/feat-f1", "--oneline").Output()
		if err == nil && strings.Contains(string(out), "stint work") {
			break // the worker's commit reached origin — completion signal works
		}
		if time.Now().After(deadline) {
			t.Fatalf("stint commit never reached origin; log:\n%s", out)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// lifecycleRunner simulates the agent: it writes + commits work in the RepoDir
// the stint hands it, then reports the launch to the inner recorder.
type lifecycleRunner struct {
	inner *fakeRunner
	t     *testing.T
}

func (l *lifecycleRunner) Run(ctx context.Context, lc orchestrate.LaunchContext) error {
	if err := os.WriteFile(filepath.Join(lc.RepoDir, "work.txt"), []byte("done"), 0o644); err != nil {
		return err
	}
	gitRun(l.t, lc.RepoDir, "add", "work.txt")
	gitRun(l.t, lc.RepoDir, "commit", "-m", "stint work by "+lc.Seat)
	return l.inner.Run(ctx, lc)
}


func TestServeOrchestrator_PolicyGate(t *testing.T) {
	dir := t.TempDir()
	srv := New([]registry.Project{{Name: "demo", Path: dir}})
	ran := make(chan []string, 1)
	srv.execOrchestrate = func(_ string, args, _ []string) error { ran <- args; return nil }
	srv.SetSeat("claude")
	o := &serveOrchestrator{s: srv}

	// No policy / stint-only policy → denied.
	if err := o.RunOrchestrate(remoteexec.OrchestrateRequest{Project: "demo"}); err == nil {
		t.Fatal("no policy should deny remote orchestrate")
	}
	writePolicy(t, dir, `{"stint":true}`)
	if err := o.RunOrchestrate(remoteexec.OrchestrateRequest{Project: "demo"}); err == nil {
		t.Fatal("stint-only policy should deny remote orchestrate")
	}

	// orchestrate:true → spawns with feature + seat kinds.
	writePolicy(t, dir, `{"orchestrate":true}`)
	if err := o.RunOrchestrate(remoteexec.OrchestrateRequest{Project: "demo", Feature: "f1", SeatKinds: map[string]string{"w": "opencode"}}); err != nil {
		t.Fatalf("orchestrate should be allowed: %v", err)
	}
	select {
	case args := <-ran:
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--feature f1") || !strings.Contains(joined, "--seat-kind w=opencode") {
			t.Fatalf("spawn args wrong: %v", args)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("orchestrate never spawned")
	}

	// Unknown project → denied.
	if err := o.RunOrchestrate(remoteexec.OrchestrateRequest{Project: "nope"}); err == nil {
		t.Fatal("unknown project should deny")
	}
}

func TestServePlanner_PolicyGate(t *testing.T) {
	dir := t.TempDir()
	srv := New([]registry.Project{{Name: "demo", Path: dir}})
	gen := make(chan []string, 1)
	srv.runPlanner = func(_ string, args, _ []string) error { gen <- args; return nil }
	srv.SetSeat("claude")
	pl := &servePlanner{s: srv}

	// No plan policy → denied.
	if err := pl.RunPlan(remoteexec.PlanRequest{Project: "demo", Feature: "f1", Goal: "g"}); err == nil {
		t.Fatal("no policy should deny remote plan")
	}
	// plan:true → generate spawns the planner with goal + feature.
	writePolicy(t, dir, `{"plan":true}`)
	if err := pl.RunPlan(remoteexec.PlanRequest{Project: "demo", Feature: "f1", Goal: "build X"}); err != nil {
		t.Fatalf("plan generate should be allowed: %v", err)
	}
	select {
	case args := <-gen:
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "build X") || !strings.Contains(joined, "--feature f1") {
			t.Fatalf("planner args wrong: %v", args)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("planner never spawned")
	}
	// Unknown project → denied.
	if err := pl.RunPlan(remoteexec.PlanRequest{Project: "nope", Feature: "f1", Goal: "g"}); err == nil {
		t.Fatal("unknown project should deny")
	}
}
