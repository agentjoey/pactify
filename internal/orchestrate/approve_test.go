package orchestrate

import (
	"testing"

	"github.com/agentjoey/pactify/internal/roles"
)

// Approving a pending proposal overrides that seat's kind FOR THIS RUN and
// records the profile as tried, so a second env failure advances the chain.
func TestApproveFallbackOverridesSeatForRun(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := roles.Load()
	if err := c.SetProfile("primary", roles.Profile{Kind: "kimi-cli", Fallback: []string{"backup"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("backup", roles.Profile{Kind: "opencode"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("w", "primary"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := writeProposal(dir, historyScopeAll, FallbackProposal{Task: "t1", Seat: "w", FromRole: "primary", ToRole: "backup", Tried: []string{"backup"}}); err != nil {
		t.Fatal(err)
	}

	opts, err := (Options{Dir: dir, ApproveFallback: []string{"t1"}}).applyApprovedFallback()
	if err != nil {
		t.Fatalf("applyApprovedFallback: %v", err)
	}

	if got := opts.kind("w"); got != "opencode" {
		t.Fatalf("approved fallback must launch seat w as the backup role's kind, got %q", got)
	}
	if tried := opts.triedFallbacks[historyScopeAll]["w"]; len(tried) != 1 || tried[0] != "backup" {
		t.Fatalf("approval must record the tried profile: %v", opts.triedFallbacks)
	}
	if _, ok := readProposal(dir, historyScopeAll); ok {
		t.Fatal("an adopted proposal must be cleared")
	}
}

// Without --approve-fallback the pending proposal is left untouched and the
// seat keeps its configured role.
func TestNoApproveLeavesProposalAndRole(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := roles.Load()
	if err := c.SetProfile("primary", roles.Profile{Kind: "kimi-cli", Fallback: []string{"backup"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("backup", roles.Profile{Kind: "opencode"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("w", "primary"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := writeProposal(dir, historyScopeAll, FallbackProposal{Task: "t1", Seat: "w", ToRole: "backup"}); err != nil {
		t.Fatal(err)
	}

	opts, err := (Options{Dir: dir}).applyApprovedFallback()
	if err != nil {
		t.Fatal(err)
	}
	if got := opts.kind("w"); got != "kimi-cli" {
		t.Fatalf("without approval the seat keeps its role kind, got %q", got)
	}
	if _, ok := readProposal(dir, historyScopeAll); !ok {
		t.Fatal("an unapproved proposal must stay pending")
	}
}
