package orchestrate

import (
	"context"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/roles"
)

// An env-class trip for a seat with a fallback chain writes a proposal.
func TestEnvClassTripWritesProposal(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := roles.Load()
	if err := c.SetProfile("primary", roles.Profile{Kind: "claude-code", Fallback: []string{"backup"}}); err != nil {
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

	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat/x", spec)

	notify := &recNotify{}
	opts := baseOpts(dir, errRunner{context.DeadlineExceeded}, &okExec{}, notify)
	opts.SeatKind = func(string) string { return "claude-code" }
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	p, ok := readProposal(dir)
	if !ok {
		t.Fatalf("env-class trip must write a fallback proposal; notify=%v", notify.msgs)
	}
	if p.Seat != "w" || p.ToRole != "backup" || p.FromRole != "primary" {
		t.Fatalf("proposal = %+v, want seat=w primary->backup", p)
	}
	if !strings.Contains(strings.Join(notify.msgs, "\n"), "orchestrate paused") {
		t.Fatalf("the run must still pause: %v", notify.msgs)
	}
}

// A seat with no role binding gets the plain escalation — no proposal.
func TestUnboundSeatWritesNoProposal(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat/x", spec)
	if err := Run(context.Background(), baseOpts(dir, errRunner{context.DeadlineExceeded}, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := readProposal(dir); ok {
		t.Fatal("an unbound seat must not get a fallback proposal")
	}
}
