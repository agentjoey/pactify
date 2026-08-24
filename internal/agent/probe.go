package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// WiringStatus is the content-aware wiring state of one registry kind for a
// given repo dir. Wired is true when EITHER the kind's config target contains
// the pact server key OR its entry file (in dir) carries a managed block that
// names this kind (see entrymark.go — entry files are shared between kinds, so
// the block's mere presence proves nothing about which kind was wired). Global
// and DocOnly are static properties of the kind (global =
// machine-level config outside the repo; doc-only = TOML config not written by
// Wire, only the snippet is shown). Via names WHICH probe proved the wiring —
// "config" (the kind's own config file) or "entry" (the kind's entry markdown),
// empty when not wired — for callers that care about the provenance, e.g.
// doctor.RelevantKinds, which discounts machine-global config wiring because it
// says nothing about THIS repo.
type WiringStatus struct {
	Kind    string `json:"kind"`
	Wired   bool   `json:"wired"`
	Via     string `json:"via,omitempty"`
	Detail  string `json:"detail"`
	Path    string `json:"path"`
	Global  bool   `json:"global"`
	DocOnly bool   `json:"docOnly"`
}

// Wiring probe sources (WiringStatus.Via).
//
// ViaEntry and ViaEntryLegacy are both "the entry file says so", but they carry
// very different confidence, and callers must not conflate them: ViaEntry means
// the managed block NAMES this kind, so the attribution is exact. Legacy means
// the block predates attribution and this kind was credited by the back-compat
// rule (doc-only, or sole tenant of that entry file) — which is right for "is it
// wired?" (a doc-only kind has no other evidence anywhere) but too loose for
// "does this project DEPEND on it?", where a wrong yes costs a spurious red and
// other signals are available. See doctor.RelevantKinds.
const (
	ViaConfig      = "config"
	ViaEntry       = "entry"
	ViaEntryLegacy = "entry-legacy"
)

// configMarker is the substring a wired config file must contain for a kind's
// format. JSON kinds key the server under "pact"; TOML kinds carry the
// mcp_servers.pact table header.
func configMarker(format Format) string {
	if format == TOML {
		return "mcp_servers.pact"
	}
	return `"pact"`
}

// probeKind computes the wiring status of a single kind against dir. The config
// probe reads the kind's ConfigTarget path (ExpandPath for Global scope) and
// checks for the format's marker; the entry probe reads the kind's DefaultEntry
// file in dir for the pact:begin managed-block marker. Wired = either.
func probeKind(kind string, dir string) WiringStatus {
	a, _ := Get(kind)
	c := a.Config()
	ws := WiringStatus{
		Kind:    kind,
		Path:    c.Path,
		Global:  c.Scope == Global,
		DocOnly: c.Format == TOML,
	}

	// Config probe: project-scoped paths are relative to dir; global paths are
	// machine-absolute (~ expanded, honoring $HOME).
	cfgPath := c.Path
	if c.Scope == Global {
		cfgPath = ExpandPath(c.Path)
	} else {
		cfgPath = filepath.Join(dir, c.Path)
	}
	if b, err := os.ReadFile(cfgPath); err == nil && strings.Contains(string(b), configMarker(c.Format)) {
		ws.Wired = true
		ws.Via = ViaConfig
		ws.Detail = "config " + c.Path
		return ws
	}

	// Entry probe: the kind's entry file in dir carries a managed block that
	// claims THIS kind. Presence alone is not enough — entry files are shared
	// (AGENTS.md by five kinds, GEMINI.md by two) and the block is kind-agnostic,
	// so `agent add opencode` used to mark every co-tenant wired. An attributed
	// block (pact:kinds line, written by WireAt) is authoritative; an older
	// unattributed block falls back to legacyEntryCredits.
	if entry := a.DefaultEntry(); entry != "" {
		if b, err := os.ReadFile(filepath.Join(dir, entry)); err == nil && hasManagedBlock(string(b)) {
			credited, via := legacyEntryCredits(entry, c), ViaEntryLegacy
			if kinds, attributed := entryKinds(string(b)); attributed {
				credited, via = slices.Contains(kinds, kind), ViaEntry
			}
			if credited {
				ws.Wired = true
				ws.Via = via
				ws.Detail = "entry " + entry
				return ws
			}
		}
	}

	ws.Detail = "not wired"
	return ws
}

// ProbeWiring returns the wiring status of every registry kind (sorted by
// Kinds()) for dir. It is the single source of truth shared by `pactify doctor`
// and the serve wiring endpoint.
func ProbeWiring(dir string) []WiringStatus {
	kinds := Kinds()
	out := make([]WiringStatus, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, probeKind(k, dir))
	}
	return out
}

// PinnedSeat is a legacy config that hard-codes a seat id in the pact server's
// env (pre-seat-identity wiring). Reported by PinnedIdentity for the doctor
// migration warning.
type PinnedSeat struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	Seat string `json:"seat"`
}

// PinnedIdentity scans dir's project-scoped JSON kinds for a pact server whose
// env pins a LITERAL PACT_AGENT_ID (spec seat-identity §3.4). An empty value or
// an expansion token (${...}) is the de-identified form and is not flagged. This
// backs `pactify doctor`'s "re-wire to drop the pinned identity" warning: a
// pinned seat blocks multiple same-kind seats and overrides orchestrate's
// injected identity.
func PinnedIdentity(dir string) []PinnedSeat {
	var out []PinnedSeat
	for _, k := range Kinds() {
		a, _ := Get(k)
		c := a.Config()
		if c.Scope != Project || c.Format == TOML {
			continue // machine-global and doc-only configs are out of scope here
		}
		b, err := os.ReadFile(filepath.Join(dir, c.Path))
		if err != nil {
			continue
		}
		var root map[string]any
		if json.Unmarshal(b, &root) != nil {
			continue
		}
		servers, _ := root[parentKey(c.Format)].(map[string]any)
		pact, _ := servers["pact"].(map[string]any)
		envKey := "env"
		if c.Format == JSONOpencode {
			envKey = "environment"
		}
		env, _ := pact[envKey].(map[string]any)
		v, _ := env["PACT_AGENT_ID"].(string)
		if v != "" && !strings.Contains(v, "${") {
			out = append(out, PinnedSeat{Kind: k, Path: c.Path, Seat: v})
		}
	}
	return out
}

// InferKindFrom derives an agent kind from a seat name by stripping a trailing
// role suffix (-worker/-reviewer/-orchestrator) and matching the resulting base
// against known. It returns the kind if it is an exact match or the unique kind
// that has the base as a prefix; "" when there is no match or it is ambiguous.
//
// This is a HEURISTIC, and the orchestrate driver's last resort for a seat that
// never recorded a kind (serve/orchestrate.go seatKindsFromFold). It lives here
// so `pactify doctor` scopes its vendor preflight to the same kinds the driver
// would actually launch — two copies would drift into two different answers.
func InferKindFrom(seat string, known []string) string {
	base := seat
	for _, sfx := range []string{"-worker", "-reviewer", "-orchestrator"} {
		if strings.HasSuffix(seat, sfx) {
			base = strings.TrimSuffix(seat, sfx)
			break
		}
	}
	// exact match first
	for _, k := range known {
		if k == base {
			return k
		}
	}
	// unique prefix match
	var match string
	for _, k := range known {
		if strings.HasPrefix(k, base) {
			if match != "" {
				return "" // ambiguous
			}
			match = k
		}
	}
	return match
}

// EntryOwners returns every registry kind whose DefaultEntry is the given
// filename, sorted. A file owned by exactly one kind (CLAUDE.md → claude-code)
// identifies that kind unambiguously; a shared one (AGENTS.md → five kinds)
// identifies none of them on its own. Exported for doctor.RelevantKinds, which
// needs it to score a pre-attribution managed block: see ViaEntryLegacy.
func EntryOwners(entry string) []string { return entryTenants(entry) }
