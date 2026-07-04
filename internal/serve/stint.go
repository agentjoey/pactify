package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/agentjoey/pactify/internal/orchestrate"
	"github.com/agentjoey/pactify/internal/remoteexec"
)

// RemotePolicy is a project's opt-in remote-execution policy, read from
// .pact/remote.json. Absent file ⇒ zero value ⇒ everything off (safe default).
// Remote agent execution runs arbitrary code, so it must be explicitly enabled
// per project.
type RemotePolicy struct {
	// Stint enables pact.stint (a remote control plane can spawn an agent here).
	Stint bool `json:"stint"`
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

// RunStint validates + accepts synchronously (policy check), then spawns the agent
// asynchronously — the agent works the task in the project's repo and checkpoints;
// the effect returns via the event stream (serve's uploader), not this call.
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
	go func() {
		_ = st.runner.Run(context.Background(), orchestrate.LaunchContext{
			Seat:     req.Seat,
			Kind:     req.AgentKind,
			Task:     req.Task,
			Project:  req.Project,
			Briefing: req.Briefing,
			RepoDir:  p.Path,
		})
	}()
	return nil
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
