package serve

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/agentjoey/pactify/internal/orchestrate"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/registry"
	"github.com/agentjoey/pactify/internal/roles"
)

// These tests cover the serve→child seat-kind CHANNEL split. serve derives seat
// kinds from configuration (init events → roster → name heuristic); the operator
// never types them. Sending them as `--seat-kind` laundered configuration into
// the child's explicit-operator-intent channel, where KIND-2 makes it displace a
// seat's role binding. Derived kinds must ride `--roster-kind` (non-explicit);
// only a caller-supplied kind (HTTP body `seat_kinds` / the orchestrate.run rpc)
// is genuine operator intent and stays on `--seat-kind`.

// seedKindRepo builds a REAL git repo + pact project in an isolated PACTIFY_HOME
// with three seats exercising all three of serve's derivation tiers:
//
//	orch            kind from the init event
//	w               kind from the init event, but ALSO bound to a role profile
//	                pinning a different kind (the regression's shape)
//	opencode-worker no kind anywhere — only serve's name heuristic resolves it
//
// A task is assigned to w on a checked-out feature branch so the driver has a
// launchable action.
func seedKindRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "base"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	os.Unsetenv("PACT_DIR")
	t.Setenv("PACT_AGENT_ID", "orch")

	p := pact.At(dir).As("orch")
	if err := p.Init("proj", []string{
		"orch:orchestrator,reviewer:CLAUDE.md:claude-code",
		"w:worker:AGENTS.md:opencode",
		"opencode-worker:worker:AGENTS.md",
	}); err != nil {
		t.Fatalf("init: %v", err)
	}

	rel := filepath.Join(".pact", "tasks", "t1.md")
	if err := os.WriteFile(filepath.Join(dir, rel), []byte("# t1\n\nverify: true\n"), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	if err := pact.At(dir).As("orch").Assign("t1", "f1", "feat-f1", "w", "orch", rel, nil); err != nil {
		t.Fatalf("assign: %v", err)
	}
	c := exec.Command("git", "checkout", "-q", "-B", "feat-f1")
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v %s", err, out)
	}
	return dir
}

// bindAgyRole binds seat to a role profile pinning the antigravity kind (binary
// `agy`) and a model. The seat's LEDGER kind is opencode, so the binding and the
// derived kind disagree — which is exactly when provenance decides the launch.
func bindAgyRole(t *testing.T, seat string) {
	t.Helper()
	c, err := roles.Load()
	if err != nil {
		t.Fatalf("roles.Load: %v", err)
	}
	if err := c.SetProfile("agy-worker", roles.Profile{Kind: "antigravity", Model: "gemini-3.1-pro-high"}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if err := c.Bind(seat, "agy-worker"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// spawnArgs POSTs an orchestrate run and returns the argv serve handed the child.
func spawnArgs(t *testing.T, dir string, body map[string]any) []string {
	t.Helper()
	fake := &fakeOrchRunner{}
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	srv.SetSeat("orch")
	srv.SetExecOrchestrate(fake.run)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/api/projects/p/orchestrate/run", body)
	defer resp.Body.Close()
	if resp.StatusCode != 202 {
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		t.Fatalf("status=%d body=%v, want 202", resp.StatusCode, m)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !fake.called {
		t.Fatal("runner should be called")
	}
	return append([]string(nil), fake.args...)
}

// flagPairs collects the values of a repeatable `--name seat=kind` flag out of
// argv and parses them with the CHILD's own parser — the same
// orchestrate.ParseSeatKinds `pactify orchestrate` uses — so this crosses the
// process boundary through the real contract rather than re-implementing it.
func flagPairs(t *testing.T, argv []string, name string) map[string]string {
	t.Helper()
	var vals []string
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "--"+name {
			vals = append(vals, argv[i+1])
		}
	}
	m, err := orchestrate.ParseSeatKinds(name, vals)
	if err != nil {
		t.Fatalf("child cannot parse serve's --%s argv %v: %v", name, vals, err)
	}
	return m
}

// The regression: every kind serve DERIVES (init events, roster, name heuristic)
// must ride the non-explicit --roster-kind channel. None of it was typed by an
// operator, so none of it may claim operator intent.
func TestServeDerivedKindsUseRosterChannel(t *testing.T) {
	dir := seedKindRepo(t)
	argv := spawnArgs(t, dir, map[string]any{})

	roster := flagPairs(t, argv, "roster-kind")
	explicit := flagPairs(t, argv, "seat-kind")

	if len(explicit) != 0 {
		t.Errorf("--seat-kind = %v, want empty: the caller supplied no kinds, so nothing is explicit operator intent", explicit)
	}
	for seat, want := range map[string]string{"orch": "claude-code", "w": "opencode"} {
		if roster[seat] != want {
			t.Errorf("--roster-kind[%s] = %q, want %q", seat, roster[seat], want)
		}
	}
}

// The name heuristic is serve's only unique contribution (init/roster kinds the
// child re-derives itself), and it IS load-bearing — this repo's own ledger has
// seats like `opencode-worker` with no kind anywhere. It must survive the split,
// on the non-explicit channel.
func TestServeNameHeuristicUsesRosterChannel(t *testing.T) {
	dir := seedKindRepo(t)
	argv := spawnArgs(t, dir, map[string]any{})

	if got := flagPairs(t, argv, "roster-kind")["opencode-worker"]; got != "opencode" {
		t.Errorf("--roster-kind[opencode-worker] = %q, want opencode (name heuristic lost)", got)
	}
	if got := flagPairs(t, argv, "seat-kind")["opencode-worker"]; got != "" {
		t.Errorf("--seat-kind[opencode-worker] = %q, want empty: a heuristic guess is not operator intent", got)
	}
}

// A kind the CALLER supplied (HTTP body `seat_kinds`, or the orchestrate.run rpc
// that feeds the same parameter) IS operator intent and must stay explicit — and
// must not be duplicated onto the derived channel, where it would be ambiguous.
func TestServeCallerSuppliedKindsStayExplicit(t *testing.T) {
	dir := seedKindRepo(t)
	argv := spawnArgs(t, dir, map[string]any{"seat_kinds": map[string]string{"w": "claude-code"}})

	explicit := flagPairs(t, argv, "seat-kind")
	roster := flagPairs(t, argv, "roster-kind")

	if explicit["w"] != "claude-code" {
		t.Errorf("--seat-kind[w] = %q, want claude-code (caller-supplied kind must stay explicit)", explicit["w"])
	}
	if _, dup := roster["w"]; dup {
		t.Errorf("--roster-kind[w] = %q, want absent: the caller's kind already owns that seat", roster["w"])
	}
	// Seats the caller said nothing about are still derived.
	if roster["orch"] != "claude-code" {
		t.Errorf("--roster-kind[orch] = %q, want claude-code", roster["orch"])
	}
}

// captureRunner is the REAL CmdRunner with its exec and warning sinks captured:
// resolution (agentcfg, role bindings, model pins) runs for real, only the
// process spawn is faked.
type captureRunner struct {
	mu       sync.Mutex
	name     string
	args     []string
	warnings []string
}

func (c *captureRunner) runner() orchestrate.CmdRunner {
	return orchestrate.CmdRunner{
		Exec: func(_ context.Context, name string, args []string, _ string, _ []string, _ io.Writer) error {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.name, c.args = name, append([]string(nil), args...)
			return nil
		},
		Warn: func(msg string) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.warnings = append(c.warnings, msg)
		},
	}
}

// driveWithServeArgs runs the child driver the way serve's spawn does: it takes
// serve's argv, re-parses it through the child's flag contract, wires the two
// channels onto Options exactly as cmd_orchestrate does, and drives one real
// launch through the real CmdRunner/agentcfg resolution. This is the seam the
// KIND-2 tests never crossed — they stopped at the driver boundary, which is why
// serve's laundering went unnoticed.
func driveWithServeArgs(t *testing.T, dir string, argv []string) *captureRunner {
	t.Helper()
	sk := flagPairs(t, argv, "seat-kind")
	rk := flagPairs(t, argv, "roster-kind")

	cap := &captureRunner{}
	opts := orchestrate.Options{
		Dir:          dir,
		Run:          cap.runner(),
		Orchestrator: "orch",
		Th:           orchestrate.Thresholds{MaxRework: 3, MaxFails: 1, MaxIters: 4},
		Now:          func() string { return "20260101-000000" },
		SeatKind:     func(seat string) string { return sk[seat] },
		RosterKind:   func(seat string) string { return rk[seat] },
	}
	// The run always ends in an escalation (the fake exec never checkpoints);
	// what matters is the launch it made on the way there.
	_ = orchestrate.Run(context.Background(), opts)

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.name == "" {
		t.Fatal("driver launched nothing — the test cannot observe a resolution")
	}
	return cap
}

// THE REGRESSION: a run spawned through serve, on a seat bound to a role
// profile, must still launch that role's agent+model. Before the split serve
// sent the seat's derived ledger kind as `--seat-kind`, the child read it as
// operator intent, and KIND-2 displaced the binding (dropping its model pin)
// on every dashboard / Dispatch / schedule / approve-fallback run.
func TestServeSpawnedRunKeepsRoleBinding(t *testing.T) {
	dir := seedKindRepo(t)
	bindAgyRole(t, "w")

	cap := driveWithServeArgs(t, dir, spawnArgs(t, dir, map[string]any{}))

	if cap.name != "agy" {
		t.Errorf("launched %q, want agy — the role binding must still decide a serve-spawned run's kind", cap.name)
	}
	if !strings.Contains(strings.Join(cap.args, " "), "gemini-3.1-pro-high") {
		t.Errorf("args %v, want the role profile's model pin gemini-3.1-pro-high", cap.args)
	}
	if len(cap.warnings) != 0 {
		t.Errorf("warnings = %v, want none: nothing was displaced, so the operator must not be told otherwise", cap.warnings)
	}
}

// The KIND-2 behavior must NOT be undone: a genuinely caller-supplied kind still
// beats the role binding, still drops the now cross-vendor model pin, and still
// warns.
func TestServeSpawnedRunHonorsCallerSuppliedKind(t *testing.T) {
	dir := seedKindRepo(t)
	bindAgyRole(t, "w")

	argv := spawnArgs(t, dir, map[string]any{"seat_kinds": map[string]string{"w": "claude-code"}})
	cap := driveWithServeArgs(t, dir, argv)

	if cap.name != "claude" {
		t.Errorf("launched %q, want claude — an operator-supplied kind must still win over the role binding", cap.name)
	}
	if len(cap.warnings) == 0 {
		t.Error("warnings = none, want the displacement warning (KIND-2 must not be undone)")
	}
}
