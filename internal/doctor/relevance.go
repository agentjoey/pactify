package doctor

import (
	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/roles"
)

// RelevantKinds is the set of agent kinds THIS project depends on — the kinds
// whose vendor CLI has to work for the repo at cwd to function. It answers the
// only question a doctor exit code is useful for: "is my setup OK for this
// project?" Everything outside the set is still reported, just advisory (see
// Check.Advisory).
//
// It is a UNION of four signals, deliberately erring toward "relevant": a false
// positive costs a red for a CLI you probably do use, a false negative silently
// hides a real break, which is strictly worse.
//
//  1. Roster kinds — projection Seat.Kind from the ledger. The seat literally
//     declares what it runs on.
//  2. Seat-name inference — a seat with no recorded kind (the common case for
//     repos initialized before `join --kind`) still routes to a vendor by name.
//     This reuses agent.InferKindFrom, the SAME heuristic the orchestrate
//     driver applies when it launches that seat, so doctor preflights exactly
//     what the driver will run.
//  3. Role bindings — a machine-level roles.json binding overrides the roster
//     kind for a seat, so the bound profile's kind is what actually launches.
//     Only bindings for seats in THIS roster count; another project's seat is
//     another project's problem. Fallback chains are NOT included: a fallback
//     only ever runs after an explicit `--approve-fallback`, so a cold one is
//     not a broken setup today.
//  4. Project wiring — an ATTRIBUTABLE pact wiring in this repo. Config wiring
//     (opencode.json, .mcp.json, .gemini/settings.json) is kind-specific and
//     counts. Entry-file wiring counts only when the entry file belongs to one
//     kind (CLAUDE.md → claude-code); a shared entry (AGENTS.md is the default
//     for five kinds) proves nothing about any single one of them, and letting
//     it vote would drag most of the registry back into the exit code — the
//     exact bug this scoping exists to fix.
//
// OUTSIDE A PACT PROJECT the ledger is empty and, absent wiring files, the set
// comes back EMPTY (never nil): no vendor CLI can gate the exit code, because
// there is no project to have a toolchain. That is not a silent pass — checkRepo
// already reports the missing .pact/ as a hard red, so `pactify doctor` in a
// random directory still exits non-zero, for the right reason.
//
// Empty and nil are different: nil is the "no scoping, everything gates" input
// VendorChecks passes for the multi-project serve preflight.
func RelevantKinds(cwd string) map[string]bool {
	out := map[string]bool{}

	for _, ws := range agent.ProbeWiring(cwd) {
		if !ws.Wired {
			continue
		}
		switch ws.Via {
		case agent.ViaConfig:
			// A machine-global config marks every repo on the box, so it is no
			// evidence about THIS one — same exclusion checkAgentWiring makes.
			if !ws.Global {
				out[ws.Kind] = true
			}
		case agent.ViaEntry:
			// The entry file is in this repo regardless of where the kind keeps
			// its config, so Global does not disqualify it. Ambiguity used to:
			// entry files are shared (AGENTS.md by five kinds) and the managed
			// block was kind-agnostic, so a shared entry proved nothing about any
			// one of them. The block now carries a pact:kinds attribution line and
			// ViaEntry means it NAMES this kind, so the verdict is exact.
			out[ws.Kind] = true

		case agent.ViaEntryLegacy:
			// A pre-attribution block. probeKind credits doc-only kinds and sole
			// tenants there by a back-compat rule — correct for "is it wired?",
			// where a doc-only kind has no other evidence on disk. For "does this
			// project DEPEND on it?" only the unambiguous half of that rule holds:
			// a file owned by exactly one kind (CLAUDE.md → claude-code) can only
			// ever mean that kind, but a legacy AGENTS.md would otherwise drag
			// BOTH doc-only co-tenants (codex-cli, codex-app) into the gating set
			// on the strength of someone once wiring opencode. Unlike wiring
			// status, relevance has other signals to fall back on (roster kind,
			// name inference, role bindings), so it takes the stricter reading.
			// Re-wiring anything with an attributing build upgrades the file and
			// the kind reappears above, legitimately.
			if a, ok := agent.Get(ws.Kind); ok && len(agent.EntryOwners(a.DefaultEntry())) == 1 {
				out[ws.Kind] = true
			}
		}
	}

	st, err := pact.At(cwd).StateProjection()
	if err != nil {
		return out // unreadable/absent ledger: wiring is all we know
	}
	// Load once; a missing or malformed roles.json is simply "no bindings"
	// (roles is advisory config and must never break a diagnostic).
	cfg, _ := roles.Load()
	known := agent.Kinds()
	for _, a := range st.Agents {
		if a.Kind != "" {
			out[a.Kind] = true
		} else if k := agent.InferKindFrom(a.ID, known); k != "" {
			out[k] = true
		}
		if p, _, ok := cfg.Lookup(a.ID); ok && p.Kind != "" {
			out[p.Kind] = true
		}
	}
	return out
}
