package serve

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/machineid"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/remoteexec"
	"github.com/agentjoey/pactify/internal/remotemachine"
)

// StartMachineChannel connects serve to the relay AS this account's machine: it
// registers presence (host + drivable agent kinds + project workdirs) and
// heartbeats it so the account's machines are visible + selectable in the web app
// (M1). When remoteControl is true it ALSO executes pact.* rpc locally (U3
// down-channel) — opt-in because that runs writes on local repos. It blocks until
// ctx is cancelled, reconnecting with capped backoff + token refresh. No-op when
// the relay uploader isn't configured (a purely-local serve is unaffected).
//
// Identity: a persistent per-host machineId (internal/machineid), distinct from
// the account — many machines per account, each addressable. Commands execute as
// the serve's acting seat (s.seat); a verb the seat may not do fails (couriered
// back as not-OK; the effect/absence shows on the board via the ledger).
func (s *Server) StartMachineChannel(ctx context.Context, remoteControl bool) {
	if s.relay == nil {
		return
	}
	mid, err := machineid.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pactify serve: machine channel disabled: %v\n", err)
		return
	}
	base := strings.TrimSuffix(s.relay.endpoint, "/v1/pact/ingest")
	info := remotemachine.Info{
		Host:       hostname(),
		AgentKinds: s.machineAgentKinds(),
		Workdirs:   s.projectPaths(),
	}
	var resolve remoteexec.Resolver
	var stint remoteexec.Stinter
	var orch remoteexec.Orchestrator
	var plan remoteexec.Planner
	var prov remoteexec.Provisioner
	if remoteControl {
		resolve = func(project string) (remoteexec.PactEngine, error) {
			s.pmu.RLock()
			p, ok := s.projects[project]
			s.pmu.RUnlock()
			if !ok {
				return nil, fmt.Errorf("unknown project %q", project)
			}
			return pact.At(p.Path).As(s.seat), nil
		}
		stint = s.newStinter() // pact.stint, gated per-project by .pact/remote.json
		orch = s.newOrchestrator() // orchestrate.run/resume, same policy file
		plan = s.newPlanner()      // plan.generate/apply, same policy file
		prov = s.newProvisioner()  // pact.provision, machine-gated by PACTIFY_PROVISION_DIR
	}

	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for ctx.Err() == nil {
		s.relay.mu.Lock()
		token := s.relay.token
		s.relay.mu.Unlock()

		_ = remotemachine.Run(ctx, remotemachine.Config{
			RelayURL:  base,
			Account:   s.relay.accountID,
			MachineID: mid,
			Token:     token,
			Info:      info,
			Resolve:   resolve,
			Stint:     stint,
			Orch:      orch,
			Plan:      plan,
			Prov:      prov,
		})
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
		}
		if sess, err := s.relay.client.Authenticate(ctx, s.relay.master); err == nil {
			s.relay.mu.Lock()
			s.relay.token = sess.Token
			s.relay.mu.Unlock()
			backoff = time.Second
		}
	}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// pactToWireKind maps pactify's launch kind keys to the relay's AgentKind enum
// (wire: opencode/claude/codex/kimi/gemini). Unknown kinds are dropped so the
// relay's MachineInfo.agentKinds validation never rejects the register.
var pactToWireKind = map[string]string{
	"opencode":    "opencode",
	"claude-code": "claude",
	"gemini-cli":  "gemini",
	"kimi-cli":    "kimi",
	"codex-cli":   "codex",
}

// machineAgentKinds is the drivable-agent roster this machine advertises, in the
// relay's AgentKind vocabulary, deduped + sorted.
func (s *Server) machineAgentKinds() []string {
	seen := map[string]bool{}
	for _, k := range agent.Kinds() {
		if w, ok := pactToWireKind[k]; ok {
			seen[w] = true
		}
	}
	out := make([]string, 0, len(seen))
	for w := range seen {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// projectPaths is this machine's registered project workdirs (presence detail).
func (s *Server) projectPaths() []string {
	s.pmu.RLock()
	defer s.pmu.RUnlock()
	out := make([]string, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, p.Path)
	}
	sort.Strings(out)
	return out
}
