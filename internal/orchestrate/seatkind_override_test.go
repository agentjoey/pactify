package orchestrate

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/roles"
)

// bindAgyProfile binds seat to a role profile pinning the antigravity kind, in
// an isolated PACTIFY_HOME.
func bindAgyProfile(t *testing.T, seat string) {
	t.Helper()
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, err := roles.Load()
	if err != nil {
		t.Fatalf("roles.Load: %v", err)
	}
	if err := c.SetProfile("agy-worker", roles.Profile{Kind: "antigravity", Model: "gemini-3.7-flash-medium"}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if err := c.Bind(seat, "agy-worker"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// KIND-2: a LaunchContext flagged KindExplicit (the operator's `--seat-kind`)
// must launch THAT kind's binary even though the seat is bound to a role whose
// profile pins a different kind — and the runner must surface a warning so the
// displacement is never silent.
func TestCmdRunner_ExplicitSeatKindBeatsRoleBinding(t *testing.T) {
	bindAgyProfile(t, "w1")
	dir := t.TempDir()

	var cap runCapture
	var warnings []string
	r := CmdRunner{
		Exec: func(_ context.Context, name string, args []string, d string, env []string, capture io.Writer) error {
			cap.called = true
			cap.name = name
			cap.args = append([]string(nil), args...)
			return nil
		},
		Warn: func(msg string) { warnings = append(warnings, msg) },
	}

	lc := LaunchContext{
		Seat: "w1", Kind: "claude-code", KindExplicit: true,
		Task: "t-k2-1", Project: "demo", Briefing: "do the work", RepoDir: dir,
	}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !cap.called {
		t.Fatal("execFn was not called")
	}
	if cap.name != "claude" {
		t.Errorf("command = %q, want claude — the explicit --seat-kind must win over the role binding", cap.name)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one override warning", warnings)
	}
	for _, want := range []string{"w1", "claude-code", "antigravity"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("warning %q must mention %q", warnings[0], want)
		}
	}
}

// Regression guard: with KindExplicit unset the kind is roster-provenance, and
// the role binding must still win — unchanged pre-KIND-2 behavior.
func TestCmdRunner_RosterKindStillYieldsToRoleBinding(t *testing.T) {
	bindAgyProfile(t, "w1")
	dir := t.TempDir()

	var cap runCapture
	var warnings []string
	r := CmdRunner{
		Exec: func(_ context.Context, name string, args []string, d string, env []string, capture io.Writer) error {
			cap.called = true
			cap.name = name
			return nil
		},
		Warn: func(msg string) { warnings = append(warnings, msg) },
	}

	lc := LaunchContext{
		Seat: "w1", Kind: "claude-code", // no KindExplicit
		Task: "t-k2-2", Project: "demo", Briefing: "do the work", RepoDir: dir,
	}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cap.name != "agy" {
		t.Errorf("command = %q, want agy — a roster-derived kind must still yield to the role binding", cap.name)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
}

// An unbound seat displaces nothing, so an explicit kind is silent.
func TestCmdRunner_ExplicitSeatKindUnboundSeatIsSilent(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()

	var cap runCapture
	var warnings []string
	r := CmdRunner{
		Exec: func(_ context.Context, name string, args []string, d string, env []string, capture io.Writer) error {
			cap.name = name
			return nil
		},
		Warn: func(msg string) { warnings = append(warnings, msg) },
	}

	lc := LaunchContext{
		Seat: "solo", Kind: "claude-code", KindExplicit: true,
		Task: "t-k2-3", Project: "demo", Briefing: "hi", RepoDir: dir,
	}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cap.name != "claude" {
		t.Errorf("command = %q, want claude", cap.name)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none — nothing was displaced", warnings)
	}
}

// The PRODUCTION runner has no injected sink (NewCmdRunner leaves Warn nil), so
// the default channel must still make the override visible: stderr, the same
// place escalate/sandbox failures land.
func TestCmdRunner_OverrideWarningDefaultsToStderr(t *testing.T) {
	bindAgyProfile(t, "w1")
	dir := t.TempDir()

	rp, wp, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = wp
	defer func() { os.Stderr = orig }()

	r := CmdRunner{Exec: func(_ context.Context, _ string, _ []string, _ string, _ []string, _ io.Writer) error {
		return nil
	}} // Warn deliberately nil — the production shape
	runErr := r.Run(context.Background(), LaunchContext{
		Seat: "w1", Kind: "claude-code", KindExplicit: true,
		Task: "t-k2-4", Project: "demo", Briefing: "hi", RepoDir: dir,
	})
	_ = wp.Close()
	os.Stderr = orig

	out, _ := io.ReadAll(rp)
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	got := string(out)
	if !strings.Contains(got, "--seat-kind") || !strings.Contains(got, "antigravity") {
		t.Errorf("stderr = %q, want the override warning naming --seat-kind and the displaced kind", got)
	}
}

// explicitKindRunner records the provenance flag each launch carried.
type explicitKindRunner struct {
	kind     string
	explicit bool
}

func (r *explicitKindRunner) Run(_ context.Context, lc LaunchContext) error {
	r.kind = lc.Kind
	r.explicit = lc.KindExplicit
	return nil
}

// The provenance flag is PLUMBED, not inferred: launchAgent marks a stint's kind
// explicit exactly when it came from Options.SeatKind (the `--seat-kind` flag),
// which is the only place in the driver that knows the operator typed it.
func TestLaunchAgentCarriesSeatKindProvenance(t *testing.T) {
	dir := t.TempDir()

	rec := &explicitKindRunner{}
	over := Options{Dir: dir, Run: rec, SeatKind: func(seat string) string {
		if seat == "w1" {
			return "claude-code"
		}
		return ""
	}}

	if err := over.launchAgent(context.Background(), "w1", over.kind("w1"), "brief", "t1", ""); err != nil {
		t.Fatalf("launchAgent: %v", err)
	}
	if rec.kind != "claude-code" || !rec.explicit {
		t.Errorf("lc = {Kind:%q KindExplicit:%v}, want {claude-code true}", rec.kind, rec.explicit)
	}

	// A seat with no --seat-kind entry: whatever kind the roster/role layer
	// produced is NOT an operator override.
	rec2 := &explicitKindRunner{}
	over.Run = rec2
	if err := over.launchAgent(context.Background(), "w2", "opencode", "brief", "t2", ""); err != nil {
		t.Fatalf("launchAgent: %v", err)
	}
	if rec2.kind != "opencode" || rec2.explicit {
		t.Errorf("lc = {Kind:%q KindExplicit:%v}, want {opencode false}", rec2.kind, rec2.explicit)
	}

	// No SeatKind function at all → never explicit.
	rec3 := &explicitKindRunner{}
	plain := Options{Dir: dir, Run: rec3}
	if err := plain.launchAgent(context.Background(), "w3", "opencode", "brief", "t3", ""); err != nil {
		t.Fatalf("launchAgent: %v", err)
	}
	if rec3.explicit {
		t.Error("KindExplicit = true with no SeatKind override configured")
	}
}
