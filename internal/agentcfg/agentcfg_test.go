package agentcfg

import (
	"reflect"
	"testing"

	"github.com/agentjoey/pactify/internal/roles"
)

func TestResolveWith_DefaultsToBuiltin(t *testing.T) {
	eff, ok := ResolveWith("claude-code", Override{})
	if !ok {
		t.Fatal("ResolveWith claude-code ok=false")
	}
	if eff.Command != "claude" {
		t.Errorf("Command = %q, want claude", eff.Command)
	}
	if eff.Model != "claude-opus-4-8" {
		t.Errorf("Model = %q, want default claude-opus-4-8", eff.Model)
	}
	want := []string{"-p", "--no-session-persistence", "--dangerously-skip-permissions", "--model", "claude-opus-4-8", "{briefing}"}
	if !reflect.DeepEqual(eff.Args, want) {
		t.Errorf("Args = %v, want %v", eff.Args, want)
	}
}

func TestResolveWith_ModelOverride(t *testing.T) {
	eff, _ := ResolveWith("opencode", Override{Model: "deepseek/deepseek-r2"})
	if eff.Model != "deepseek/deepseek-r2" {
		t.Errorf("Model = %q, want override", eff.Model)
	}
	want := []string{"run", "-m", "deepseek/deepseek-r2", "{briefing}"}
	if !reflect.DeepEqual(eff.Args, want) {
		t.Errorf("Args = %v, want %v", eff.Args, want)
	}
}

func TestResolveWith_ScopedPermissions(t *testing.T) {
	eff, _ := ResolveWith("claude-code", Override{Restricted: true, AllowedTools: []string{"Read", "Edit", "Bash"}})
	if !eff.Scoped {
		t.Error("Scoped = false, want true")
	}
	want := []string{"-p", "--no-session-persistence", "--allowedTools", "Read,Edit,Bash", "--model", "claude-opus-4-8", "{briefing}"}
	if !reflect.DeepEqual(eff.Args, want) {
		t.Errorf("Args = %v, want %v", eff.Args, want)
	}
}

// claude-desktop is the non-drivable example here — antigravity is now the CLI
// binary agy and IS drivable (agy-kind task, 2026-08-22); see
// TestResolveWith_AntigravityDrivable.
func TestResolveWith_NonDrivable(t *testing.T) {
	if _, ok := ResolveWith("claude-desktop", Override{}); ok {
		t.Error("expected ok=false for non-drivable claude-desktop")
	}
}

// antigravity: EffortArgs is deliberately nil (verified 2026-08-22 — agy
// hard-errors on a mismatched --model tier suffix / --effort pair), so an
// Override.Effort must be a no-op — Args stay byte-for-byte what BuildArgs
// rendered, same as any kind with no declared EffortArgs.
func TestResolveWith_AntigravityDrivable(t *testing.T) {
	eff, ok := ResolveWith("antigravity", Override{})
	if !ok {
		t.Fatal("ResolveWith antigravity ok=false")
	}
	if eff.Command != "agy" {
		t.Errorf("Command = %q, want agy", eff.Command)
	}
	if eff.Model != "gemini-3.7-flash-medium" {
		t.Errorf("Model = %q, want default gemini-3.7-flash-medium", eff.Model)
	}
	want := []string{"-p", "{briefing}", "--model", "gemini-3.7-flash-medium",
		"--add-dir", "{repoDir}", "--output-format", "json",
		"--dangerously-skip-permissions", "--print-timeout", "30m"}
	if !reflect.DeepEqual(eff.Args, want) {
		t.Errorf("Args = %v, want %v", eff.Args, want)
	}

	effWithEffort, _ := ResolveWith("antigravity", Override{Effort: "low"})
	if !reflect.DeepEqual(effWithEffort.Args, want) {
		t.Errorf("Effort override must no-op (EffortArgs nil): Args = %v, want unchanged %v", effWithEffort.Args, want)
	}
	if effWithEffort.Effort != "" {
		t.Errorf("Effort = %q, want empty (no EffortArgs declared)", effWithEffort.Effort)
	}
}

// The placeholder agentcfg emits must be the exact token orchestrate substitutes.
func TestPlaceholderStable(t *testing.T) {
	if Placeholder != "{briefing}" {
		t.Errorf("Placeholder = %q, want {briefing}", Placeholder)
	}
	eff, _ := ResolveWith("opencode", Override{})
	found := false
	for _, a := range eff.Args {
		if a == Placeholder {
			found = true
		}
	}
	if !found {
		t.Errorf("resolved args %v missing placeholder %q", eff.Args, Placeholder)
	}
}

// Two seats of the SAME kind must be able to run different models — the
// limitation per-kind resolution could not express.
func TestResolveSeatGivesSameKindSeatsDifferentModels(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := roles.Load()
	if err := c.SetProfile("pro", roles.Profile{Kind: "opencode", Model: "deepseek/deepseek-v4-pro"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("cheap", roles.Profile{Kind: "opencode", Model: "deepseek/deepseek-v4-flash"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("w1", "pro"); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("w2", "cheap"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	e1, ok1 := ResolveSeat("w1", "opencode", "")
	e2, ok2 := ResolveSeat("w2", "opencode", "")
	if !ok1 || !ok2 {
		t.Fatalf("both seats must resolve: %v %v", ok1, ok2)
	}
	if e1.Model != "deepseek/deepseek-v4-pro" || e2.Model != "deepseek/deepseek-v4-flash" {
		t.Fatalf("same-kind seats must differ by profile: w1=%q w2=%q", e1.Model, e2.Model)
	}
}

// An unbound seat resolves exactly as before roles existed (per-kind path).
func TestResolveSeatUnboundMatchesResolve(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	want, okWant := Resolve("opencode")
	got, okGot := ResolveSeat("unbound-seat", "opencode", "")
	if okGot != okWant || got.Model != want.Model || got.Command != want.Command {
		t.Fatalf("unbound seat must match Resolve: got %+v(%v) want %+v(%v)", got, okGot, want, okWant)
	}
}

// Effort is an append-only side channel: for a kind that declares EffortArgs,
// Override.Effort renders the CLI's effort flag(s) AFTER BuildArgs' output.
func TestResolveWith_EffortAppended(t *testing.T) {
	eff, ok := ResolveWith("claude-code", Override{Effort: "low"})
	if !ok {
		t.Fatal("ResolveWith claude-code ok=false")
	}
	want := []string{"-p", "--no-session-persistence", "--dangerously-skip-permissions", "--model", "claude-opus-4-8", "{briefing}", "--effort", "low"}
	if !reflect.DeepEqual(eff.Args, want) {
		t.Errorf("Args = %v, want %v", eff.Args, want)
	}
	if eff.Effort != "low" {
		t.Errorf("Effort = %q, want low", eff.Effort)
	}

	eff, _ = ResolveWith("codex-cli", Override{Effort: "high"})
	want = []string{"exec", "--json", "--sandbox", "danger-full-access", "{briefing}", "-c", "model_reasoning_effort=high"}
	if !reflect.DeepEqual(eff.Args, want) {
		t.Errorf("codex Args = %v, want %v", eff.Args, want)
	}
}

// A kind with NO declared EffortArgs ignores effort entirely: its resolved args
// stay byte-for-byte identical to the zero-override launch, and Effective.Effort
// reports "" (nothing was injected).
func TestResolveWith_EffortIgnoredWithoutEffortArgs(t *testing.T) {
	base, _ := ResolveWith("opencode", Override{})
	got, _ := ResolveWith("opencode", Override{Effort: "high"})
	if !reflect.DeepEqual(got.Args, base.Args) {
		t.Errorf("opencode Args = %v, want byte-identical to %v", got.Args, base.Args)
	}
	if got.Effort != "" {
		t.Errorf("Effort = %q, want \"\" (no EffortArgs → nothing injected)", got.Effort)
	}
}

// Effort == "" must leave Args byte-for-byte unchanged.
func TestResolveWith_EmptyEffortUnchanged(t *testing.T) {
	base, _ := ResolveWith("claude-code", Override{})
	got, _ := ResolveWith("claude-code", Override{Effort: ""})
	if !reflect.DeepEqual(got.Args, base.Args) {
		t.Errorf("Args = %v, want byte-identical to %v", got.Args, base.Args)
	}
}

// The tier-derived budget reaches the launch of an unbound seat (per-kind path).
func TestResolveSeat_TierEffortApplied(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	eff, ok := ResolveSeat("unbound-seat", "claude-code", "low")
	if !ok {
		t.Fatal("ResolveSeat ok=false")
	}
	if eff.Effort != "low" {
		t.Fatalf("Effort = %q, want low", eff.Effort)
	}
	last := eff.Args[len(eff.Args)-2:]
	if !reflect.DeepEqual(last, []string{"--effort", "low"}) {
		t.Errorf("Args tail = %v, want [--effort low]", last)
	}
}

// An explicit per-seat profile Effort ALWAYS wins over the tier-derived value —
// the operator's explicit setting must remain absolute (execution-tiering §4.5).
// A profile without an Effort pin follows the tier.
func TestResolveSeat_ExplicitProfileEffortWins(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := roles.Load()
	if err := c.SetProfile("pinned", roles.Profile{Kind: "claude-code", Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("plain", roles.Profile{Kind: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("w-pinned", "pinned"); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("w-plain", "plain"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	pinned, ok := ResolveSeat("w-pinned", "claude-code", "low")
	if !ok {
		t.Fatal("ResolveSeat w-pinned ok=false")
	}
	if pinned.Effort != "high" {
		t.Errorf("pinned seat Effort = %q, want explicit profile pin \"high\" to win over tier \"low\"", pinned.Effort)
	}

	plain, ok := ResolveSeat("w-plain", "claude-code", "low")
	if !ok {
		t.Fatal("ResolveSeat w-plain ok=false")
	}
	if plain.Effort != "low" {
		t.Errorf("unpinned seat Effort = %q, want tier-derived \"low\"", plain.Effort)
	}
}

// A seat bound to the antigravity "frontend" role (docs/operations.md's
// recommended catalog) must resolve to agy with the profile's model and NO
// --effort flag — even when the caller passes a tierEffort, because the
// profile's Effort is "" and agentcfg only ever injects a flag when
// EffortArgs is non-nil (nil for antigravity, per agy-kind's hard-error
// finding: a mismatched --model/--effort pair exits 1).
func TestResolveSeat_AntigravityRoleBinding(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := roles.Load()
	if err := c.SetProfile("frontend", roles.Profile{Kind: "antigravity", Model: "gemini-3.7-flash-medium"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("ops", roles.Profile{Kind: "antigravity", Model: "gemini-3.7-flash-low"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("agy-1", "frontend"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	// tierEffort="high" must NOT survive into Args: antigravity has no
	// EffortArgs, so ResolveWith's append-only side channel is a no-op.
	eff, ok := ResolveSeat("agy-1", "antigravity", "high")
	if !ok {
		t.Fatal("ResolveSeat agy-1 ok=false")
	}
	if eff.Command != "agy" {
		t.Errorf("Command = %q, want agy", eff.Command)
	}
	if eff.Model != "gemini-3.7-flash-medium" {
		t.Errorf("Model = %q, want gemini-3.7-flash-medium (the frontend role's model)", eff.Model)
	}
	if eff.Effort != "" {
		t.Errorf("Effort = %q, want empty — antigravity must never carry an --effort flag", eff.Effort)
	}
	for _, a := range eff.Args {
		if a == "--effort" {
			t.Fatalf("Args must never contain --effort for antigravity (hard-errors on mismatched tier): %v", eff.Args)
		}
	}
}

// Defining the antigravity role profiles is not the same as binding a seat to
// one: an UNBOUND seat must resolve exactly as it did before agy-roles landed
// — i.e. identically to the plain per-kind Resolve — even though the
// "frontend"/"ops" profiles now exist in roles.json. This is the hard
// regression requirement (byte-for-byte unchanged for projects with no role
// bound) exercised specifically against the new antigravity catalog.
func TestResolveSeat_AntigravityProfilesDefinedButUnboundMatchesResolve(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := roles.Load()
	if err := c.SetProfile("frontend", roles.Profile{Kind: "antigravity", Model: "gemini-3.7-flash-medium"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("test", roles.Profile{Kind: "antigravity", Model: "gemini-3.7-flash-medium"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("ops", roles.Profile{Kind: "antigravity", Model: "gemini-3.7-flash-low"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("docs", roles.Profile{Kind: "antigravity", Model: "gemini-3.7-flash-low"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	want, okWant := Resolve("antigravity")
	got, okGot := ResolveSeat("some-other-seat", "antigravity", "")
	if okGot != okWant || !reflect.DeepEqual(got, want) {
		t.Fatalf("unbound seat with profiles defined-but-unbound must match Resolve byte-for-byte: got %+v(%v) want %+v(%v)", got, okGot, want, okWant)
	}
}

func TestResolveWith_PopulatesKind(t *testing.T) {
	eff, ok := ResolveWith("claude-code", Override{})
	if !ok {
		t.Fatal("ResolveWith ok=false")
	}
	if eff.Kind != "claude-code" {
		t.Errorf("eff.Kind = %q, want claude-code", eff.Kind)
	}

	effAgy, ok := ResolveWith("antigravity", Override{})
	if !ok {
		t.Fatal("ResolveWith antigravity ok=false")
	}
	if effAgy.Kind != "antigravity" {
		t.Errorf("effAgy.Kind = %q, want antigravity", effAgy.Kind)
	}
}

func TestResolveSeat_EffectiveKind(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := roles.Load()
	if err := c.SetProfile("claude-reviewer", roles.Profile{Kind: "claude-code", Model: "claude-opus-4-8"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("agy-worker", roles.Profile{Kind: "antigravity", Model: "gemini-3.7-flash-medium"}); err != nil {
		t.Fatal(err)
	}
	c.Profiles["no-kind-override"] = roles.Profile{Model: "custom-model"}
	if err := c.Bind("reviewer", "claude-reviewer"); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("worker", "agy-worker"); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("fallback", "no-kind-override"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	// 1. Seat bound to claude-code profile, but passed kind="antigravity" -> Effective.Kind must be "claude-code"
	eff, ok := ResolveSeat("reviewer", "antigravity", "")
	if !ok {
		t.Fatal("ResolveSeat reviewer ok=false")
	}
	if eff.Kind != "claude-code" {
		t.Errorf("reviewer eff.Kind = %q, want claude-code", eff.Kind)
	}

	// 2. Seat bound to antigravity profile, passed kind="claude-code" -> Effective.Kind must be "antigravity"
	eff, ok = ResolveSeat("worker", "claude-code", "")
	if !ok {
		t.Fatal("ResolveSeat worker ok=false")
	}
	if eff.Kind != "antigravity" {
		t.Errorf("worker eff.Kind = %q, want antigravity", eff.Kind)
	}

	// 3. Seat bound to profile with empty Kind -> fallback to passed kind
	eff, ok = ResolveSeat("fallback", "opencode", "")
	if !ok {
		t.Fatal("ResolveSeat fallback ok=false")
	}
	if eff.Kind != "opencode" {
		t.Errorf("fallback eff.Kind = %q, want opencode", eff.Kind)
	}

	// 4. Unbound seat -> Effective.Kind is passed kind
	eff, ok = ResolveSeat("unbound", "claude-code", "")
	if !ok {
		t.Fatal("ResolveSeat unbound ok=false")
	}
	if eff.Kind != "claude-code" {
		t.Errorf("unbound eff.Kind = %q, want claude-code", eff.Kind)
	}
}
