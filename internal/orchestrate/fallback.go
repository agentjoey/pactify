package orchestrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agentjoey/pactify/internal/roles"
)

// FallbackProposal is the machine-readable half of an env-class escalation: the
// driver's suggestion to run this seat under a different role profile. The
// escalation .md stays the human-readable half; this sidecar is what the
// dashboard renders and what `--approve-fallback <task>` adopts.
//
// One proposal per SCOPE (see proposalPath), not one per run: a
// --max-concurrency > 1 run pauses each feature independently, so several
// proposals can be pending at once and a single file would have them overwrite
// each other.
type FallbackProposal struct {
	Task     string   `json:"task"`
	Seat     string   `json:"seat"`
	FromRole string   `json:"fromRole"`
	ToRole   string   `json:"toRole"`
	Reason   string   `json:"reason"`
	Tried    []string `json:"tried"`
}

// scopedProposal pairs a proposal with the scope whose file holds it. The scope
// is not in the JSON — it IS the filename — but every consumer needs it:
// approval installs the role override under that scope, and serve renders one
// card per scope.
type scopedProposal struct {
	Scope string
	P     FallbackProposal
}

// proposalDir is <runtimeDir>/.pact/orchestrate/fallback/, the sibling of the
// history/ and parallel/ per-scope directories.
func proposalDir(runtimeDir string) string {
	return filepath.Join(runtimeDir, ".pact", "orchestrate", "fallback")
}

// proposalPath is <runtimeDir>/.pact/orchestrate/fallback/<scope>.json, where
// scope is a feature id (parallel: one per concurrent feature; serial: the
// --feature filter) or "all" for an unfiltered serial run. The scope is reduced
// to a bare filename so a stray id cannot escape the directory — the same guard
// historyPath and writeFeatureStatus use.
func proposalPath(runtimeDir, scope string) string {
	name := filepath.Base(scope)
	if name == "" || name == "." || name == ".." {
		name = historyScopeAll
	}
	return filepath.Join(proposalDir(runtimeDir), name+".json")
}

// legacyProposalPath is the pre-FALLBACK-PAR single-file location. Nothing reads
// it any more; clearProposals deletes it in passing so a stale proposal written
// by an older binary cannot sit on disk forever.
func legacyProposalPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, ".pact", "orchestrate", "fallback-proposal.json")
}

// writeProposal persists scope's proposal atomically (temp + rename, like
// writeStatus/writeHistory) so a dashboard poll can never read a half-written
// invitation to swap agents. The temp file is <scope>.json.tmp, which the
// *.json aggregators skip.
func writeProposal(runtimeDir, scope string, p FallbackProposal) error {
	final := proposalPath(runtimeDir, scope)
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// readProposal loads scope's proposal. Absent, unreadable, malformed, or missing
// either of the two fields an approval acts on (seat, toRole) all mean "nothing
// pending" — fail-closed, because a proposal we cannot fully understand must
// never become an approval.
func readProposal(runtimeDir, scope string) (FallbackProposal, bool) {
	return decodeProposal(proposalPath(runtimeDir, scope))
}

func decodeProposal(path string) (FallbackProposal, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return FallbackProposal{}, false
	}
	var p FallbackProposal
	if json.Unmarshal(b, &p) != nil || p.Seat == "" || p.ToRole == "" {
		return FallbackProposal{}, false
	}
	return p, true
}

// loadProposals returns every pending proposal, sorted by scope. A missing
// directory is an empty list (no run has proposed anything yet); an individual
// file that does not decode is skipped, exactly like the parallel status
// aggregator treats a half-written status.
func loadProposals(runtimeDir string) []scopedProposal {
	entries, err := os.ReadDir(proposalDir(runtimeDir))
	if err != nil {
		return nil
	}
	var out []scopedProposal
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p, ok := decodeProposal(filepath.Join(proposalDir(runtimeDir), e.Name()))
		if !ok {
			continue
		}
		out = append(out, scopedProposal{Scope: strings.TrimSuffix(e.Name(), ".json"), P: p})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Scope < out[j].Scope })
	return out
}

// clearProposals removes the named scopes' proposal files, and in passing the
// legacy single-file path (§2.1): the new code never reads it, so leaving it
// behind would strand a stale proposal on disk for good. Best-effort — a
// proposal that cannot be deleted is at worst re-offered, never acted on twice
// (the run that adopted it already holds the override in memory).
func clearProposals(runtimeDir string, scopes []string) {
	for _, s := range scopes {
		_ = os.Remove(proposalPath(runtimeDir, s))
	}
	_ = os.Remove(legacyProposalPath(runtimeDir))
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

// applyApprovedFallback consumes the proposals the operator named with
// `--approve-fallback <task>` (repeatable): each seat runs under its proposed
// role FOR THIS RUN, the profile is marked tried so a repeat failure advances
// the chain, and the proposal file is cleared. Without approval nothing changes
// and the proposals stay pending for the dashboard or a later CLI approval.
//
// The override is installed under the proposal's own SCOPE, never run-wide: two
// parallel features can share one seat, and a run-level override would apply an
// approval the operator gave for feature A to feature B as well (§2.3).
//
// aliasScopes additionally installs the approval under scopes the CALLER knows
// are the same decision. The serial driver passes its own run scope: serial has
// exactly one scope, so an approval typed on the command line must take effect
// even when the proposal was filed under a different --feature filter — which is
// precisely what serve's approve does, since it resumes without one. Parallel
// passes none: there, distinct scopes are distinct decisions.
//
// A task with no pending proposal is an ERROR and nothing is applied: silently
// continuing would let the operator believe the agent was swapped while the run
// burns another whole budget cycle on the same configuration (§2.2).
func (opts Options) applyApprovedFallback(aliasScopes ...string) (Options, error) {
	if len(opts.ApproveFallback) == 0 {
		return opts, nil
	}
	pending := loadProposals(opts.runtimeDir())
	byTask := map[string]scopedProposal{}
	for _, sp := range pending {
		byTask[sp.P.Task] = sp
	}

	var missing []string
	adopt := make([]scopedProposal, 0, len(opts.ApproveFallback))
	for _, task := range opts.ApproveFallback {
		sp, ok := byTask[task]
		if !ok {
			missing = append(missing, task)
			continue
		}
		adopt = append(adopt, sp)
	}
	if len(missing) > 0 {
		// Resolve every name BEFORE touching anything: a half-applied approval
		// (one seat swapped, one file consumed, then an error) is worse than none.
		have := "no fallback proposal is pending"
		if len(pending) > 0 {
			names := make([]string, 0, len(pending))
			for _, sp := range pending {
				names = append(names, sp.P.Task)
			}
			sort.Strings(names)
			have = "pending proposals: " + strings.Join(names, ", ")
		}
		return opts, fmt.Errorf("orchestrate: --approve-fallback %s: no such pending fallback proposal (%s)",
			strings.Join(missing, ", "), have)
	}

	if opts.roleOverride == nil {
		opts.roleOverride = map[string]map[string]string{}
	}
	if opts.triedFallbacks == nil {
		opts.triedFallbacks = map[string]map[string][]string{}
	}
	consumed := make([]string, 0, len(adopt))
	for _, sp := range adopt {
		for _, scope := range dedup(append([]string{sp.Scope}, aliasScopes...)) {
			if opts.roleOverride[scope] == nil {
				opts.roleOverride[scope] = map[string]string{}
			}
			opts.roleOverride[scope][sp.P.Seat] = sp.P.ToRole
			if opts.triedFallbacks[scope] == nil {
				opts.triedFallbacks[scope] = map[string][]string{}
			}
			opts.triedFallbacks[scope][sp.P.Seat] = append(opts.triedFallbacks[scope][sp.P.Seat], sp.P.Tried...)
		}
		consumed = append(consumed, sp.Scope)
	}
	clearProposals(opts.runtimeDir(), consumed)
	return opts, nil
}

// dedup returns ss with duplicates removed, order preserved.
func dedup(ss []string) []string {
	seen := map[string]bool{}
	out := ss[:0:0]
	for _, s := range ss {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
