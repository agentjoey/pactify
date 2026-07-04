package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/orchestrate"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/remoteexec"
)

// RemotePolicy is a project's opt-in remote-execution policy, read from
// .pact/remote.json. Absent file ⇒ zero value ⇒ everything off (safe default).
// Remote agent execution runs arbitrary code, so it must be explicitly enabled
// per project.
type RemotePolicy struct {
	// Stint enables pact.stint (a remote control plane can spawn an agent here).
	Stint bool `json:"stint"`
	// Orchestrate enables orchestrate.run/resume rpc (drive this machine's
	// orchestrate remotely). Reserved for the M3/M4 remote-orchestrate entry.
	Orchestrate bool `json:"orchestrate"`
	// AgentKinds optionally restricts which agent kinds may be spawned remotely;
	// empty ⇒ any known kind.
	AgentKinds []string `json:"agentKinds,omitempty"`
}

func readRemotePolicy(dir string) RemotePolicy {
	b, err := os.ReadFile(filepath.Join(dir, ".pact", "remote.json"))
	if err != nil {
		return RemotePolicy{}
	}
	var p RemotePolicy
	if json.Unmarshal(b, &p) != nil {
		return RemotePolicy{} // malformed ⇒ deny
	}
	return p
}

// serveStinter runs remote agent stints for a Server, gated by each project's
// RemotePolicy. It spawns the agent via the same orchestrate runner the local
// driver uses (identical launch/audit path).
type serveStinter struct {
	s      *Server
	runner orchestrate.Runner
}

// RunStint validates + accepts synchronously (policy check), then runs the stint
// lifecycle asynchronously. Multi-machine semantics (branch-as-interface): when
// the project has an `origin` remote and the task's feature branch is known, the
// stint runs in an ISOLATED WORKTREE of that branch — fetch origin first (pick up
// the driver's pushed assign/ledger), run the agent there, push the branch back
// (the agent's checkpoint commits become visible to the dispatching driver's
// poll). Without a remote (single-machine setup) it degrades to running in the
// project tree and the effect stays local.
func (st *serveStinter) RunStint(req remoteexec.StintRequest) error {
	st.s.pmu.RLock()
	p, ok := st.s.projects[req.Project]
	st.s.pmu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown project %q", req.Project)
	}
	pol := readRemotePolicy(p.Path)
	if !pol.Stint {
		return errors.New("remote stint not enabled for this project (see .pact/remote.json)")
	}
	if len(pol.AgentKinds) > 0 && !contains(pol.AgentKinds, req.AgentKind) {
		return fmt.Errorf("agent kind %q not permitted for remote stint here", req.AgentKind)
	}
	go st.runLifecycle(p.Path, req)
	return nil
}

// runLifecycle executes one stint end-to-end. Errors are best-effort logged —
// the remote driver's completion signal is the pushed checkpoint (or its absence,
// which trips the driver's poll timeout), never this goroutine's outcome.
func (st *serveStinter) runLifecycle(repoDir string, req remoteexec.StintRequest) {
	dir := repoDir
	branch := ""
	hasRemote := gitx.HasRemote(repoDir, "origin")
	if hasRemote {
		// Pick up the driver's latest push (the assign + ledger this stint works).
		if err := gitx.Fetch(repoDir, "origin", ""); err != nil {
			stintLog(req, "fetch origin: %v", err)
		}
		// The rpc carries the branch (the driver knows it); fall back to this
		// machine's ledger for the single-machine/local case.
		branch = req.Branch
		if branch == "" {
			branch = taskBranch(repoDir, req.Task)
		}
	}
	if branch != "" {
		// Isolated worktree on the feature branch, based on its origin tip, so the
		// stint never disturbs whatever the machine's main tree is doing.
		wt := filepath.Join(repoDir, ".pact", "orchestrate", "wt", "stint-"+req.Task)
		base := "origin/" + branch
		if !gitx.RemoteBranchExists(repoDir, "origin", branch) {
			base = gitx.DefaultBranch(repoDir)
		}
		if err := gitx.AddWorktree(repoDir, wt, branch, base); err != nil {
			stintLog(req, "add worktree (running in project tree instead): %v", err)
		} else {
			dir = wt
			defer func() { _ = gitx.RemoveWorktree(repoDir, wt) }()
		}
	}

	_ = st.runner.Run(context.Background(), orchestrate.LaunchContext{
		Seat:     req.Seat,
		Kind:     req.AgentKind,
		Task:     req.Task,
		Project:  req.Project,
		Briefing: req.Briefing,
		RepoDir:  dir,
	})

	// Branch-as-interface: publish the agent's commits (checkpoint + ledger) so
	// the dispatching driver's poll sees them. Best-effort — a push failure just
	// means the driver times out and retries/escalates.
	if hasRemote && branch != "" {
		if err := gitx.Push(dir, "origin", branch); err != nil {
			stintLog(req, "push %s: %v", branch, err)
		}
	}
}

// taskBranch resolves the feature branch the task belongs to from the project's
// ledger ("" when unknown — e.g. the assign hasn't reached this machine yet).
func taskBranch(repoDir, task string) string {
	stp, err := pact.At(repoDir).StateProjection()
	if err != nil {
		return ""
	}
	for _, f := range stp.Features {
		for _, t := range f.Tasks {
			if t.ID == task {
				return f.Branch
			}
		}
	}
	return ""
}

func stintLog(req remoteexec.StintRequest, format string, args ...any) {
	fmt.Fprintf(os.Stderr, "pactify stint %s/%s: %s\n", req.Project, req.Task, fmt.Sprintf(format, args...))
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// newStinter builds the policy-gated stint executor with the standard runner.
func (s *Server) newStinter() remoteexec.Stinter {
	return &serveStinter{s: s, runner: orchestrate.NewCmdRunner(5 * time.Minute)}
}

// serveOrchestrator starts the orchestrate driver from a remote rpc
// (orchestrate.run/resume), gated by the project's RemotePolicy.Orchestrate.
// Reuses the same spawn path as the dashboard's Run button.
type serveOrchestrator struct{ s *Server }

func (o *serveOrchestrator) RunOrchestrate(req remoteexec.OrchestrateRequest) error {
	o.s.pmu.RLock()
	p, ok := o.s.projects[req.Project]
	o.s.pmu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown project %q", req.Project)
	}
	if !readRemotePolicy(p.Path).Orchestrate {
		return errors.New("remote orchestrate not enabled for this project (see .pact/remote.json)")
	}
	if _, err := o.s.spawnOrchestrate(p.Path, req.Feature, req.SeatKinds, 0, req.Resume); err != nil {
		return err
	}
	return nil
}

// newOrchestrator builds the policy-gated remote orchestrate entry.
func (s *Server) newOrchestrator() remoteexec.Orchestrator {
	return &serveOrchestrator{s: s}
}
