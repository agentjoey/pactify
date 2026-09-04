package orchestrate

import (
	"context"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/acp"
	"github.com/agentjoey/pactify/internal/roles"
)

// bindSeatProfile pins one role profile to one seat in an isolated PACTIFY_HOME.
func bindSeatProfile(t *testing.T, seat, role string, p roles.Profile) {
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

// okStint returns a fake connection that completes one trivial turn.
func okStint() *fakeAcpConn {
	fc := newFakeAcpConn()
	fc.prompt = func(fc *fakeAcpConn) (acp.StopReason, error) {
		fc.emitUpdate(acp.SessionUpdate{Kind: "agent_message_chunk"})
		return "end_turn", nil
	}
	return fc
}

// [ACP-MODEL]: the ACP transport never consults agentcfg.ResolveSeat and
// acpCommand carries no --model, so a role binding's model pin is silently
// dropped and the agent runs on its own global default. opencode defaults to
// ACP, so this is the common case, not an edge one. Until the pin is actually
// honored, the operator must at least be told.
func TestAcpRunWarnsWhenRoleBindingPinsAModel(t *testing.T) {
	bindSeatProfile(t, "w1", "mm", roles.Profile{Kind: "opencode", Model: "minimax/MiniMax-M3"})

	var warnings []string
	r := AcpRunner{Spawn: captureSpawn(okStint(), nil), Warn: func(m string) { warnings = append(warnings, m) }}

	if err := r.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "opencode", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(warnings) != 1 {
		t.Fatalf("want exactly one warning, got %d: %v", len(warnings), warnings)
	}
	w := warnings[0]
	for _, want := range []string{"w1", "minimax/MiniMax-M3", "--transport opencode=cmd"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning must mention %q, got: %s", want, w)
		}
	}
}

// No pin, nothing to warn about — a bound seat that only names a kind gets the
// kind it asked for, so the warning would be pure noise on every ACP stint.
func TestAcpRunSilentWhenBindingHasNoModelPin(t *testing.T) {
	bindSeatProfile(t, "w1", "plain", roles.Profile{Kind: "opencode"})

	var warnings []string
	r := AcpRunner{Spawn: captureSpawn(okStint(), nil), Warn: func(m string) { warnings = append(warnings, m) }}

	if err := r.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "opencode", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unbound model must not warn, got: %v", warnings)
	}
}

func TestAcpRunSilentForUnboundSeat(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())

	var warnings []string
	r := AcpRunner{Spawn: captureSpawn(okStint(), nil), Warn: func(m string) { warnings = append(warnings, m) }}

	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Kind: "kimi-cli", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("seat with no role binding must not warn, got: %v", warnings)
	}
}

// The warning is about the pin being dropped, so it must fire once per stint,
// before the agent is spawned — not after a turn has already run on the wrong
// model.
func TestAcpModelPinWarningPrecedesSpawn(t *testing.T) {
	bindSeatProfile(t, "w1", "mm", roles.Profile{Kind: "opencode", Model: "minimax/MiniMax-M3"})

	var order []string
	spawn := func(ctx context.Context, command string, args, env []string, dir string) (acpConn, error) {
		order = append(order, "spawn")
		return okStint(), nil
	}
	r := AcpRunner{Spawn: spawn, Warn: func(string) { order = append(order, "warn") }}

	if err := r.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "opencode", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(order) != 2 || order[0] != "warn" {
		t.Fatalf("warning must come before spawn, got %v", order)
	}
}
