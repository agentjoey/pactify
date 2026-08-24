package orchestrate

import (
	"testing"

	"github.com/agentjoey/pactify/internal/roles"
)

func TestNextFallbackWalksTheChain(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := roles.Load()
	if err := c.SetProfile("primary", roles.Profile{Kind: "kimi-cli", Fallback: []string{"second", "third"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("second", roles.Profile{Kind: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("third", roles.Profile{Kind: "opencode"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("w", "primary"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	to, from, ok := nextFallback("w", nil)
	if !ok || to != "second" || from != "primary" {
		t.Fatalf("first fallback = (%q,%q,%v), want (second,primary,true)", to, from, ok)
	}
	to, _, ok = nextFallback("w", []string{"second"})
	if !ok || to != "third" {
		t.Fatalf("second fallback = (%q,%v), want (third,true)", to, ok)
	}
	if _, _, ok := nextFallback("w", []string{"second", "third"}); ok {
		t.Fatal("an exhausted chain must report ok=false")
	}
	if _, _, ok := nextFallback("unbound", nil); ok {
		t.Fatal("an unbound seat has no fallback")
	}
}

func TestProposalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := readProposal(dir, historyScopeAll); ok {
		t.Fatal("no proposal should exist yet")
	}
	p := FallbackProposal{Task: "t1", Seat: "w", FromRole: "primary", ToRole: "second", Reason: "env failure", Tried: []string{"second"}}
	if err := writeProposal(dir, historyScopeAll, p); err != nil {
		t.Fatal(err)
	}
	got, ok := readProposal(dir, historyScopeAll)
	if !ok || got.Task != "t1" || got.ToRole != "second" || len(got.Tried) != 1 {
		t.Fatalf("round-trip lost data: %+v (ok=%v)", got, ok)
	}
	clearProposals(dir, []string{historyScopeAll})
	if _, ok := readProposal(dir, historyScopeAll); ok {
		t.Fatal("cleared proposal must be gone")
	}
}
