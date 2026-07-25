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

	e1, ok1 := ResolveSeat("w1", "opencode")
	e2, ok2 := ResolveSeat("w2", "opencode")
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
	got, okGot := ResolveSeat("unbound-seat", "opencode")
	if okGot != okWant || got.Model != want.Model || got.Command != want.Command {
		t.Fatalf("unbound seat must match Resolve: got %+v(%v) want %+v(%v)", got, okGot, want, okWant)
	}
}
