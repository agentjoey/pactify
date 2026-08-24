package doctor

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// isolateHome points PACTIFY_HOME at a scratch dir so roles.Load() never reads
// the developer's real ~/.pactify/roles.json.
func isolateHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("PACTIFY_HOME", h)
	return h
}

// newPactProject creates a git repo + initialized pact ledger carrying
// seatSpecs, chdirs into it, and returns the process-resolved cwd (checkRepo
// insists cwd == os.Getwd()). The acting seat is the first spec's id.
func newPactProject(t *testing.T, seatSpecs ...string) string {
	t.Helper()
	if len(seatSpecs) == 0 {
		t.Fatal("newPactProject needs at least one seat spec")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = cwd
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(cwd, "base.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", cwd, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if out, err := exec.Command("git", "-C", cwd, "commit", "-q", "-m", "base").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v %s", err, out)
	}
	t.Setenv("PACT_AGENT_ID", strings.SplitN(seatSpecs[0], ":", 2)[0])
	if err := pact.Init("p", seatSpecs); err != nil {
		t.Fatal(err)
	}
	return cwd
}

// writeRoles drops a machine-level roles.json under PACTIFY_HOME binding seat to
// a profile of the given kind.
func writeRoles(t *testing.T, home, seat, role, kind string) {
	t.Helper()
	cfg := map[string]any{
		"profiles": map[string]any{role: map[string]any{"kind": kind}},
		"bindings": map[string]any{seat: role},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "roles.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRelevantKinds_RosterKind(t *testing.T) {
	isolateHome(t)
	dir := newPactProject(t, "w:worker:AGENTS.md:codex-cli")
	got := RelevantKinds(dir)
	if !got["codex-cli"] {
		t.Fatalf("roster Seat.Kind must make the kind relevant: %v", got)
	}
	if got["gemini-cli"] || got["opencode"] {
		t.Fatalf("kinds the project never names must not be relevant: %v", got)
	}
}

// A seat with no recorded kind still routes to a vendor CLI by name (the same
// heuristic the orchestrate driver uses), so it must count.
func TestRelevantKinds_SeatNameHeuristic(t *testing.T) {
	isolateHome(t)
	dir := newPactProject(t, "gemini-worker:worker:AGENTS.md")
	got := RelevantKinds(dir)
	if !got["gemini-cli"] {
		t.Fatalf("seat named gemini-worker must make gemini-cli relevant: %v", got)
	}
}

// A machine-level role binding overrides the roster kind for a seat, so the
// bound profile's kind is what actually gets launched.
func TestRelevantKinds_RoleBinding(t *testing.T) {
	home := isolateHome(t)
	dir := newPactProject(t, "w:worker:AGENTS.md")
	writeRoles(t, home, "w", "impl", "kimi-cli")
	got := RelevantKinds(dir)
	if !got["kimi-cli"] {
		t.Fatalf("role binding kind must be relevant: %v", got)
	}
}

// A role bound to a seat that is NOT in this project's roster is another
// project's business and must not leak in.
func TestRelevantKinds_RoleBindingForForeignSeatIgnored(t *testing.T) {
	home := isolateHome(t)
	dir := newPactProject(t, "w:worker:AGENTS.md:opencode")
	writeRoles(t, home, "somebody-elses-seat", "impl", "codex-cli")
	got := RelevantKinds(dir)
	if got["codex-cli"] {
		t.Fatalf("binding for a foreign seat must not make codex-cli relevant: %v", got)
	}
}

// Kind-specific config wiring is an explicit "I use this here" statement.
func TestRelevantKinds_ConfigWiring(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	t.Chdir(dir)
	dir, _ = os.Getwd()
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{"mcp":{"pact":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := RelevantKinds(dir)
	if !got["opencode"] {
		t.Fatalf("opencode.json pact wiring must make opencode relevant: %v", got)
	}
}

// An entry file owned by exactly one kind (CLAUDE.md → claude-code) attributes.
func TestRelevantKinds_UniqueEntryWiring(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	t.Chdir(dir)
	dir, _ = os.Getwd()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("<!-- pact:begin -->\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := RelevantKinds(dir)
	if !got["claude-code"] {
		t.Fatalf("wired CLAUDE.md must make claude-code relevant: %v", got)
	}
}

// AGENTS.md is the default entry of five kinds, so it attributes to none of
// them — otherwise one shared file would drag every AGENTS.md kind back into
// the exit code, which is the bug this whole change exists to kill.
func TestRelevantKinds_SharedEntryWiringAttributesToNobody(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	t.Chdir(dir)
	dir, _ = os.Getwd()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("<!-- pact:begin -->\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := RelevantKinds(dir)
	for _, k := range []string{"opencode", "codex-cli", "kimi-cli"} {
		if got[k] {
			t.Fatalf("shared AGENTS.md must not attribute to %s: %v", k, got)
		}
	}
}

// Outside any pact repo there is nothing to depend on: the set is empty (but
// non-nil — nil means "everything", the serve-preflight contract).
func TestRelevantKinds_NoProjectIsEmptyNotNil(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	t.Chdir(dir)
	dir, _ = os.Getwd()
	got := RelevantKinds(dir)
	if got == nil {
		t.Fatal("RelevantKinds must return a non-nil (empty) set outside a project")
	}
	if len(got) != 0 {
		t.Fatalf("no project => no relevant kinds, got %v", got)
	}
}

// --- Run() wiring: relevance drives Advisory, never visibility --------------

func vendorCheck(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	c, ok := findCheck(checks, name)
	if !ok {
		t.Fatalf("check %q must still be REPORTED even when irrelevant; got %v", name, checks)
	}
	return c
}

// Regression guard: a kind the project DOES use, missing, is a hard failure.
func TestRun_RelevantVendorFailureIsGating(t *testing.T) {
	isolateHome(t)
	dir := newPactProject(t, "w:worker:AGENTS.md:codex-cli")
	emptyPath := t.TempDir()
	checks := Run(dir, "w", filepath.Join(emptyPath, "pactify"), emptyPath, t.TempDir())

	c := vendorCheck(t, checks, "cli codex-cli: binary")
	if c.OK {
		t.Fatalf("codex binary is absent from the fake PATH: %+v", c)
	}
	if c.Advisory {
		t.Fatalf("a kind the project USES must gate the exit code: %+v", c)
	}
	a := vendorCheck(t, checks, "cli codex-cli: auth")
	if a.OK || a.Advisory {
		t.Fatalf("used-kind auth failure must gate the exit code: %+v", a)
	}
}

// The reported bug: a vendor this project never touches must not fail doctor.
func TestRun_IrrelevantVendorFailureIsAdvisoryButStillShown(t *testing.T) {
	isolateHome(t)
	dir := newPactProject(t, "w:worker:AGENTS.md:codex-cli")
	emptyPath := t.TempDir()
	checks := Run(dir, "w", filepath.Join(emptyPath, "pactify"), emptyPath, t.TempDir())

	c := vendorCheck(t, checks, "cli gemini-cli: binary")
	if c.OK {
		t.Fatalf("gemini binary is absent from the fake PATH: %+v", c)
	}
	if !c.Advisory {
		t.Fatalf("a kind the project does NOT use must not gate the exit code: %+v", c)
	}
	if !strings.Contains(c.Detail, "not used by this project") {
		t.Fatalf("an advisory red must SAY why it is not fatal: %+v", c)
	}
}

// Nothing outside the vendor block ever becomes advisory: the core checks
// (.pact/, seat, wiring, PATH) always gate.
func TestRun_CoreChecksAreNeverAdvisory(t *testing.T) {
	isolateHome(t)
	dir := newPactProject(t, "w:worker:AGENTS.md:codex-cli")
	emptyPath := t.TempDir()
	for _, c := range Run(dir, "w", filepath.Join(emptyPath, "pactify"), emptyPath, t.TempDir()) {
		if strings.HasPrefix(c.Name, "cli ") {
			continue
		}
		if c.Advisory {
			t.Fatalf("core check must never be advisory: %+v", c)
		}
	}
}

// Outside any pact project every vendor is advisory — the exit code is then
// carried entirely by the core checks (".pact/ present" is already red there).
func TestRun_NoProjectMakesEveryVendorAdvisory(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	t.Chdir(dir)
	dir, _ = os.Getwd()
	emptyPath := t.TempDir()
	checks := Run(dir, "", filepath.Join(emptyPath, "pactify"), emptyPath, t.TempDir())

	sawVendor, sawGatingCore := false, false
	for _, c := range checks {
		if strings.HasPrefix(c.Name, "cli ") {
			sawVendor = true
			if !c.OK && !c.Advisory {
				t.Fatalf("outside a project no vendor may gate the exit code: %+v", c)
			}
			continue
		}
		if !c.OK {
			sawGatingCore = true
		}
	}
	if !sawVendor {
		t.Fatal("vendor checks must still be reported outside a project")
	}
	if !sawGatingCore {
		t.Fatal("a dir with no .pact must still fail doctor on the core checks")
	}
}

// Mixed: the used kind is healthy, an unused kind is broken => nothing gates.
func TestRun_MixedRelevantOKIrrelevantBrokenHasNoGatingFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	dir := newPactProject(t, "w:worker:AGENTS.md:opencode")

	pathDir := t.TempDir()
	mkExec(t, pathDir, "opencode") // the ONLY vendor binary present
	// opencode needs no credentials, so the used kind is fully green.
	pathEnv := pathDir + string(os.PathListSeparator) + filepath.Dir(filepath.Join(pathDir, "pactify"))
	mkExec(t, pathDir, "pactify")

	checks := Run(dir, "w", filepath.Join(pathDir, "pactify"), pathEnv, home)
	for _, c := range checks {
		if !c.OK && !c.Advisory {
			t.Fatalf("nothing should gate here, but %+v does", c)
		}
	}
	// ...and the broken unused vendors are still on screen.
	if c := vendorCheck(t, checks, "cli codex-cli: binary"); c.OK || !c.Advisory {
		t.Fatalf("codex-cli should be shown as a non-gating red: %+v", c)
	}
}

// VendorChecks (no relevance argument) is the serve-preflight contract: report
// everything, mark nothing advisory.
func TestVendorChecks_DefaultReportsEverythingAsGating(t *testing.T) {
	for _, c := range VendorChecks(t.TempDir(), t.TempDir()) {
		if c.Advisory {
			t.Fatalf("relevance-unaware VendorChecks must not mark anything advisory: %+v", c)
		}
	}
}
