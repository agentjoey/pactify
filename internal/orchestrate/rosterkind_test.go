package orchestrate

import (
	"context"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/roles"
)

// RosterKind is the SPAWNER-derived channel (`--roster-kind`, written by
// `pactify serve`). It must sit at the very bottom of the precedence list: its
// init/roster tiers only duplicate what the child re-derives live, so the one
// thing it uniquely contributes — serve's seat-name heuristic — is a last resort
// and must never shadow live state or a role binding.
func TestRosterKindIsLowestPriority(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := newProject(t) // roster: orch (no kind), w (no kind)

	opts := Options{Dir: dir, RosterKind: func(seat string) string {
		if seat == "w" {
			return "opencode" // what serve's name heuristic guessed
		}
		return ""
	}}

	// Nothing else resolves w → the spawner's hint is used. This is the case the
	// heuristic exists for (a seat whose kind is nowhere in the ledger).
	if k := opts.kind("w"); k != "opencode" {
		t.Fatalf("kind(w) = %q, want opencode from the spawner hint", k)
	}

	// The seat now declares a kind: LIVE state outranks the spawn-time snapshot,
	// so a mid-run `join --kind` still takes effect (spec §6 WS-K).
	if err := pact.At(dir).As("w").JoinKind("w", "worker", "kimi-cli"); err != nil {
		t.Fatalf("JoinKind: %v", err)
	}
	if k := opts.kind("w"); k != "kimi-cli" {
		t.Fatalf("kind(w) = %q, want kimi-cli — a stale spawner hint must not shadow the live roster", k)
	}

	// A role binding outranks both.
	c, _ := roles.Load()
	if err := c.SetProfile("agy-worker", roles.Profile{Kind: "antigravity"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("w", "agy-worker"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if k := opts.kind("w"); k != "antigravity" {
		t.Fatalf("kind(w) = %q, want antigravity — the role binding outranks the spawner hint", k)
	}

	// And an explicit --seat-kind still beats everything.
	opts.SeatKind = func(string) string { return "claude-code" }
	if k := opts.kind("w"); k != "claude-code" {
		t.Fatalf("kind(w) = %q, want claude-code — --seat-kind must still win", k)
	}
}

// A kind that arrived via the spawner channel is NOT operator intent, so the
// stint must not be tagged KindExplicit — that flag is what lets agentcfg
// displace a bound role profile (KIND-2).
func TestRosterKindLaunchesAsNonExplicit(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()

	rec := &explicitKindRunner{}
	opts := Options{Dir: dir, Run: rec, RosterKind: func(string) string { return "opencode" }}

	if err := opts.launchAgent(context.Background(), "w", opts.kind("w"), "brief", "t1", ""); err != nil {
		t.Fatalf("launchAgent: %v", err)
	}
	if rec.kind != "opencode" {
		t.Errorf("Kind = %q, want opencode", rec.kind)
	}
	if rec.explicit {
		t.Error("KindExplicit = true for a --roster-kind value; only --seat-kind is operator intent")
	}
}
