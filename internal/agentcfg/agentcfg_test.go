package agentcfg

import (
	"reflect"
	"testing"
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
