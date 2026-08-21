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

func TestResolveWith_NonDrivable(t *testing.T) {
	if _, ok := ResolveWith("antigravity", Override{}); ok {
		t.Error("expected ok=false for non-drivable antigravity")
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
