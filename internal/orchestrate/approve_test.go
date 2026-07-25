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
	if err := writeProposal(dir, FallbackProposal{Task: "t1", Seat: "w", FromRole: "primary", ToRole: "backup", Tried: []string{"backup"}}); err != nil {
		t.Fatal(err)
	}

	opts := Options{Dir: dir, ApproveFallback: true, triedFallbacks: map[string][]string{}}
	opts = opts.applyApprovedFallback()

	if got := opts.kind("w"); got != "opencode" {
		t.Fatalf("approved fallback must launch seat w as the backup role's kind, got %q", got)
	}
	if len(opts.triedFallbacks["w"]) != 1 || opts.triedFallbacks["w"][0] != "backup" {
		t.Fatalf("approval must record the tried profile: %v", opts.triedFallbacks)
	}
	if _, ok := readProposal(dir); ok {
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
	if err := writeProposal(dir, FallbackProposal{Task: "t1", Seat: "w", ToRole: "backup"}); err != nil {
		t.Fatal(err)
	}

	opts := Options{Dir: dir, triedFallbacks: map[string][]string{}}.applyApprovedFallback()
	if got := opts.kind("w"); got != "kimi-cli" {
		t.Fatalf("without approval the seat keeps its role kind, got %q", got)
	}
	if _, ok := readProposal(dir); !ok {
		t.Fatal("an unapproved proposal must stay pending")
	}
}
