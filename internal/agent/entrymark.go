package agent

import (
	"slices"
	"sort"
	"strings"
)

// Entry-file kind attribution.
//
// Several kinds share one entry file — AGENTS.md is the entry of opencode,
// codex-cli, kimi-cli, cursor-cli and codex-app; GEMINI.md of gemini-cli and
// antigravity. The managed block pactify bakes there is deliberately
// seat-agnostic AND kind-agnostic (briefing() names neither), so its mere
// PRESENCE proves only that something pact-ish was wired into this file, not
// which kind. Probing on presence alone made a single `pactify agent add
// opencode` report four never-configured co-tenants as wired.
//
// So WireAt records the wired kinds inside the block on a machine-readable
// HTML-comment line (invisible in rendered markdown), and probeKind checks
// membership instead of presence. The line lives INSIDE the managed block on
// purpose: it is pactify-owned state, so a re-bake rewrites it and a user
// deleting the block deletes the claim with it.

const (
	kindsMarkerPrefix = "<!-- pact:kinds:"
	kindsMarkerSuffix = "-->"
)

// kindsMarker renders the attribution line for kinds (assumed sorted+deduped).
// It must never contain the pact:begin/pact:end markers, or pact.BakeManagedBlock
// would reject it and stripBlock would mis-detect the block boundary.
func kindsMarker(kinds []string) string {
	return kindsMarkerPrefix + " " + strings.Join(kinds, ",") + " " + kindsMarkerSuffix
}

// entryBody is the managed-block body for an entry file: the attribution line
// followed by the shared seat-agnostic briefing. Pure, and byte-stable for a
// given kind set, so re-wiring stays idempotent.
func entryBody(kinds []string) string {
	return kindsMarker(kinds) + "\n\n" + briefing()
}

// hasManagedBlock reports whether content carries a pactify managed block.
func hasManagedBlock(content string) bool { return strings.Contains(content, "pact:begin") }

// entryKinds parses the attribution line from content's managed block. The
// second return is false when the file has no attribution line — either there is
// no managed block at all, or the block was baked by a pactify that predates
// attribution (see legacyEntryCredits for how that case is scored).
func entryKinds(content string) ([]string, bool) {
	inBlock := false
	for _, ln := range strings.Split(content, "\n") {
		switch {
		case strings.Contains(ln, "pact:begin"):
			inBlock = true
			continue
		case strings.Contains(ln, "pact:end"):
			inBlock = false
			continue
		case !inBlock:
			continue
		}
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, kindsMarkerPrefix) || !strings.HasSuffix(t, kindsMarkerSuffix) {
			continue
		}
		list := strings.TrimSuffix(strings.TrimPrefix(t, kindsMarkerPrefix), kindsMarkerSuffix)
		var kinds []string
		for _, k := range strings.Split(list, ",") {
			if k = strings.TrimSpace(k); k != "" {
				kinds = append(kinds, k)
			}
		}
		sort.Strings(kinds)
		return kinds, true
	}
	return nil, false
}

// mergeEntryKinds unions kind into an existing attribution (sorted, deduped), so
// wiring a second kind into a shared entry file adds to the claim instead of
// replacing it.
func mergeEntryKinds(existing []string, kind string) []string {
	out := append([]string{kind}, existing...)
	sort.Strings(out)
	return slices.Compact(out)
}

// entryOnlyKind reports whether the entry file is the ONLY artifact WireAt
// writes for this kind: TOML kinds are doc-only (WireAt returns before touching
// a config — the caller just prints the snippet), and a custom manifest kind may
// declare no MCP config at all. For these, an entry-file marker is the sole
// possible wiring signal on disk.
func entryOnlyKind(c ConfigTarget) bool { return c.Format == TOML || c.Path == "" }

// entryTenants returns every registry kind whose entry file is `entry`.
func entryTenants(entry string) []string {
	var out []string
	for k, s := range registry {
		if s.entry == entry {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// legacyEntryCredits scores an UNATTRIBUTED managed block (baked before entry
// attribution existed) for kind. Back-compat decision: credit it only where no
// other signal could ever exist, so upgrading pactify never silently un-wires a
// kind that has no second channel:
//
//   - entry-only kinds (doc-only TOML / no MCP config): the entry file is their
//     only signal; refusing it would regress every codex user;
//   - the sole tenant of that entry file (e.g. claude-code owns CLAUDE.md): a
//     block there is unambiguous by construction.
//
// Every other kind sharing the file writes its own config, which the config
// probe already checks — that is exactly what `agent add` produced for it, so
// the credit is not needed and would only reinstate the false positive.
func legacyEntryCredits(entry string, c ConfigTarget) bool {
	return entryOnlyKind(c) || len(entryTenants(entry)) == 1
}
