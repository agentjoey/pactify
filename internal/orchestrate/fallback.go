package orchestrate

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/roles"
)

// FallbackProposal is the machine-readable half of an env-class escalation: the
// driver's suggestion to run this seat under a different role profile. The
// escalation .md stays the human-readable half; this sidecar is what the
// dashboard renders and what `--approve-fallback` adopts. At most one is
// pending at a time, because a proposal only exists while the run is paused.
type FallbackProposal struct {
	Task     string   `json:"task"`
	Seat     string   `json:"seat"`
	FromRole string   `json:"fromRole"`
	ToRole   string   `json:"toRole"`
	Reason   string   `json:"reason"`
	Tried    []string `json:"tried"`
}

func proposalPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, ".pact", "orchestrate", "fallback-proposal.json")
}

func writeProposal(runtimeDir string, p FallbackProposal) error {
	path := proposalPath(runtimeDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func readProposal(runtimeDir string) (FallbackProposal, bool) {
	b, err := os.ReadFile(proposalPath(runtimeDir))
	if err != nil {
		return FallbackProposal{}, false
	}
	var p FallbackProposal
	if json.Unmarshal(b, &p) != nil || p.Seat == "" {
		return FallbackProposal{}, false
	}
	return p, true
}

func clearProposal(runtimeDir string) error {
	err := os.Remove(proposalPath(runtimeDir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// nextFallback resolves the seat's bound role and returns the first entry of
// its fallback chain that this run has not tried yet. ok=false when the seat is
// unbound, has no chain, or the chain is exhausted — the caller then writes a
// plain escalation with no proposal.
func nextFallback(seat string, tried []string) (toRole, fromRole string, ok bool) {
	cfg, err := roles.Load()
	if err != nil {
		return "", "", false
	}
	p, role, bound := cfg.Lookup(seat)
	if !bound {
		return "", "", false
	}
	seen := map[string]bool{}
	for _, t := range tried {
		seen[t] = true
	}
	for _, cand := range p.Fallback {
		if seen[cand] {
			continue
		}
		if _, defined := cfg.Profiles[cand]; !defined {
			continue // a dangling chain entry is skipped, not fatal
		}
		return cand, role, true
	}
	return "", "", false
}
