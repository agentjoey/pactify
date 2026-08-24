package agentcfg

import (
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/roles"
)

// bindProfile is the shared fixture for the KIND-2 cases: an isolated
// PACTIFY_HOME with one role profile bound to one seat.
func bindProfile(t *testing.T, seat, role string, p roles.Profile) {
	t.Helper()
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, err := roles.Load()
	if err != nil {
		t.Fatalf("roles.Load: %v", err)
	}
	if err := c.SetProfile(role, p); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if err := c.Bind(seat, role); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// KIND-2, the bug: an EXPLICIT `--seat-kind w1=claude-code` must beat the kind
// of the role profile bound to w1 — a typed flag outranks configuration, which
// is what Options.SeatKind has always documented. The displacement must also be
// visible (a returned warning the caller surfaces), never silent.
func TestResolveSeatFrom_ExplicitKindBeatsProfileKind(t *testing.T) {
	bindProfile(t, "w1", "p", roles.Profile{Kind: "antigravity", Model: "gemini-3.7-flash-medium"})

	eff, warn, ok := ResolveSeatFrom("w1", "claude-code", KindExplicit, "")
	if !ok {
		t.Fatal("ResolveSeatFrom ok=false")
	}
	if eff.Kind != "claude-code" {
		t.Errorf("Kind = %q, want claude-code (explicit --seat-kind must win over profile kind antigravity)", eff.Kind)
	}
	if eff.Command != "claude" {
		t.Errorf("Command = %q, want claude", eff.Command)
	}
	// The profile's model names an antigravity model; carrying it onto
	// claude-code would hard-fail the launch, so the displaced profile's model
	// pin must be dropped in favor of the overriding kind's own default.
	if eff.Model != "claude-opus-4-8" {
		t.Errorf("Model = %q, want claude-code's own default claude-opus-4-8 (the displaced profile's model pin must not travel across kinds)", eff.Model)
	}
	if warn == "" {
		t.Fatal("warning = \"\", want a visible warning that the explicit --seat-kind displaced the role profile's kind")
	}
	for _, want := range []string{"w1", "claude-code", "antigravity"} {
		if !strings.Contains(warn, want) {
			t.Errorf("warning %q must mention %q", warn, want)
		}
	}
}

// Regression guard: WITHOUT an explicit override the role binding must still
// beat the roster's kind — that precedence is the entire point of role binding
// and this fix must not change it.
func TestResolveSeatFrom_RosterKindStillYieldsToProfileKind(t *testing.T) {
	bindProfile(t, "w1", "p", roles.Profile{Kind: "antigravity", Model: "gemini-3.7-flash-medium"})

	eff, warn, ok := ResolveSeatFrom("w1", "claude-code", KindFromRoster, "")
	if !ok {
		t.Fatal("ResolveSeatFrom ok=false")
	}
	if eff.Kind != "antigravity" {
		t.Errorf("Kind = %q, want antigravity (a roster-derived kind must still yield to the role binding)", eff.Kind)
	}
	if eff.Command != "agy" {
		t.Errorf("Command = %q, want agy", eff.Command)
	}
	if eff.Model != "gemini-3.7-flash-medium" {
		t.Errorf("Model = %q, want the profile's pin", eff.Model)
	}
	if warn != "" {
		t.Errorf("warning = %q, want none — nothing the operator typed was displaced", warn)
	}

	// The 3-arg ResolveSeat is the roster-provenance shorthand; it must agree.
	legacy, okLegacy := ResolveSeat("w1", "claude-code", "")
	if !okLegacy || legacy.Kind != "antigravity" {
		t.Errorf("ResolveSeat = %+v(%v), want the same antigravity resolution", legacy, okLegacy)
	}
}

// No role binding at all: the explicit kind is simply used, and nothing was
// displaced, so there is nothing to warn about.
func TestResolveSeatFrom_ExplicitKindUnboundSeatNoWarning(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())

	eff, warn, ok := ResolveSeatFrom("unbound", "claude-code", KindExplicit, "")
	if !ok {
		t.Fatal("ResolveSeatFrom ok=false")
	}
	if eff.Kind != "claude-code" || eff.Command != "claude" {
		t.Errorf("Kind/Command = %q/%q, want claude-code/claude", eff.Kind, eff.Command)
	}
	if warn != "" {
		t.Errorf("warning = %q, want none — an unbound seat displaces nothing", warn)
	}
	// Byte-for-byte identical to the roster-provenance path: explicitness only
	// matters when there is a profile kind to displace.
	roster, _, _ := ResolveSeatFrom("unbound", "claude-code", KindFromRoster, "")
	if eff.Command != roster.Command || eff.Model != roster.Model || len(eff.Args) != len(roster.Args) {
		t.Errorf("explicit resolution %+v must equal roster resolution %+v for an unbound seat", eff, roster)
	}
}

// An explicit override that AGREES with the bound profile's kind displaces
// nothing, so it must stay warning-free — and must keep the profile's model
// pin, since the profile still describes the kind actually launching.
func TestResolveSeatFrom_ExplicitKindAgreesWithProfileNoWarning(t *testing.T) {
	bindProfile(t, "w1", "p", roles.Profile{Kind: "opencode", Model: "deepseek/deepseek-v4-pro"})

	eff, warn, ok := ResolveSeatFrom("w1", "opencode", KindExplicit, "")
	if !ok {
		t.Fatal("ResolveSeatFrom ok=false")
	}
	if warn != "" {
		t.Errorf("warning = %q, want none — the explicit kind matches the profile's kind", warn)
	}
	if eff.Kind != "opencode" {
		t.Errorf("Kind = %q, want opencode", eff.Kind)
	}
	if eff.Model != "deepseek/deepseek-v4-pro" {
		t.Errorf("Model = %q, want the profile's pin kept (nothing was displaced)", eff.Model)
	}
}

// A profile that pins no Kind is kind-agnostic: it decorates whatever kind the
// caller supplies, explicit or not, and never warns.
func TestResolveSeatFrom_ProfileWithoutKindNeverWarns(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := roles.Load()
	c.Profiles["kindless"] = roles.Profile{Model: "claude-sonnet-4-6"}
	if err := c.Bind("w1", "kindless"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	eff, warn, ok := ResolveSeatFrom("w1", "claude-code", KindExplicit, "")
	if !ok {
		t.Fatal("ResolveSeatFrom ok=false")
	}
	if warn != "" {
		t.Errorf("warning = %q, want none — the profile pins no kind to displace", warn)
	}
	if eff.Kind != "claude-code" || eff.Model != "claude-sonnet-4-6" {
		t.Errorf("Kind/Model = %q/%q, want claude-code/claude-sonnet-4-6", eff.Kind, eff.Model)
	}
}

// The profile's EXPLICIT Effort pin is vendor-neutral (a tier word, not a model
// name), so it survives a kind override — unchanged from the pre-KIND-2
// contract that a per-seat effort pin is absolute.
func TestResolveSeatFrom_ProfileEffortSurvivesKindOverride(t *testing.T) {
	bindProfile(t, "w1", "p", roles.Profile{Kind: "opencode", Model: "deepseek/deepseek-v4-pro", Effort: "high"})

	eff, warn, ok := ResolveSeatFrom("w1", "claude-code", KindExplicit, "low")
	if !ok {
		t.Fatal("ResolveSeatFrom ok=false")
	}
	if warn == "" {
		t.Error("want a warning: the explicit kind displaced the profile's opencode kind")
	}
	if eff.Effort != "high" {
		t.Errorf("Effort = %q, want the profile's absolute pin high", eff.Effort)
	}
}
