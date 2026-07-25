package orchestrate

import (
	"testing"

	"github.com/agentjoey/pactify/internal/roles"
)

// A seat bound to a role launches with that role's kind; the CLI --seat-kind
// override still outranks it.
func TestKindResolutionPrecedence(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := roles.Load()
	if err := c.SetProfile("frontend", roles.Profile{Kind: "claude-code", Model: "claude-opus-4-8"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("w2", "frontend"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	// (2) role binding decides when there is no CLI override.
	opts := Options{Dir: t.TempDir()}
	if got := opts.kind("w2"); got != "claude-code" {
		t.Fatalf("bound seat should resolve to its role kind, got %q", got)
	}
	// (1) CLI override still wins.
	opts.SeatKind = func(string) string { return "opencode" }
	if got := opts.kind("w2"); got != "opencode" {
		t.Fatalf("--seat-kind must outrank the role binding, got %q", got)
	}
	// (3) unbound seat is unaffected by roles.
	opts.SeatKind = nil
	if got := opts.kind("unbound"); got != "" {
		t.Fatalf("an unbound seat with no roster entry should resolve empty, got %q", got)
	}
}
