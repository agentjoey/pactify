package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/roles"
)

// bindFallbackRoles configures a machine-level role chain primary → backup and
// binds seat `w` to primary, in a PACTIFY_HOME that dies with the test.
func bindFallbackRoles(t *testing.T) {
	t.Helper()
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
}

// Proposals live one file per scope, so two concurrent features never overwrite
// each other's pending decision.
func TestProposalIsStoredPerScope(t *testing.T) {
	dir := t.TempDir()
	if err := writeProposal(dir, "fa", FallbackProposal{Task: "ta", Seat: "w", ToRole: "backup"}); err != nil {
		t.Fatal(err)
	}
	if err := writeProposal(dir, "fb", FallbackProposal{Task: "tb", Seat: "w", ToRole: "backup"}); err != nil {
		t.Fatal(err)
	}
	for scope, task := range map[string]string{"fa": "ta", "fb": "tb"} {
		p, ok := readProposal(dir, scope)
		if !ok || p.Task != task {
			t.Fatalf("scope %s must hold its own proposal, got %+v ok=%v", scope, p, ok)
		}
		if _, err := os.Stat(filepath.Join(dir, ".pact", "orchestrate", "fallback", scope+".json")); err != nil {
			t.Fatalf("proposal for %s not at fallback/%s.json: %v", scope, scope, err)
		}
	}
	got := loadProposals(dir)
	if len(got) != 2 || got[0].Scope != "fa" || got[1].Scope != "fb" {
		t.Fatalf("loadProposals must return both, sorted by scope: %+v", got)
	}
}

// A scope id can never escape the fallback directory (same guard as history).
func TestProposalScopeCannotEscapeDir(t *testing.T) {
	dir := t.TempDir()
	if err := writeProposal(dir, "../../evil", FallbackProposal{Task: "t", Seat: "w", ToRole: "backup"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.json")); err == nil {
		t.Fatal("a stray scope id must not write outside the fallback dir")
	}
}

// An unreadable / half-written / field-less proposal is "nothing pending": a
// proposal is an invitation to swap agents, so anything we cannot fully
// understand must not become an approval.
func TestProposalFailClosedOnMalformed(t *testing.T) {
	for name, body := range map[string]string{
		"not json":     `{`,
		"no seat":      `{"task":"t1","toRole":"backup"}`,
		"no target":    `{"task":"t1","seat":"w"}`,
		"empty object": `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, ".pact", "orchestrate", "fallback")
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p, "all.json"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, ok := readProposal(dir, "all"); ok {
				t.Fatalf("%s must not read as a pending proposal", name)
			}
			if got := loadProposals(dir); len(got) != 0 {
				t.Fatalf("%s must be skipped by loadProposals, got %+v", name, got)
			}
		})
	}
}

// THE scope-isolation guard (spec §2.3). Two parallel features share seat `w`.
// Approving ONLY feature fa's task must swap the agent for fa and leave fb
// running exactly the role the operator never touched.
func TestApproveFallbackIsScopedToTheApprovedFeature(t *testing.T) {
	bindFallbackRoles(t)
	dir := t.TempDir()
	if err := writeProposal(dir, "fa", FallbackProposal{Task: "ta", Seat: "w", FromRole: "primary", ToRole: "backup", Tried: []string{"backup"}}); err != nil {
		t.Fatal(err)
	}
	if err := writeProposal(dir, "fb", FallbackProposal{Task: "tb", Seat: "w", FromRole: "primary", ToRole: "backup", Tried: []string{"backup"}}); err != nil {
		t.Fatal(err)
	}

	opts, err := (Options{Dir: dir, ApproveFallback: []string{"ta"}}).applyApprovedFallback()
	if err != nil {
		t.Fatalf("applyApprovedFallback: %v", err)
	}

	fa, fb := opts, opts
	fa.Feature, fb.Feature = "fa", "fb"
	if got := fa.kind("w"); got != "opencode" {
		t.Fatalf("approved feature fa must run seat w as the backup role's kind, got %q", got)
	}
	if got := fb.kind("w"); got != "claude-code" {
		t.Fatalf("feature fb was never approved and must keep its bound role's kind, got %q", got)
	}
	if len(fa.triedFallbacks["fa"]["w"]) != 1 {
		t.Fatalf("fa must record the tried profile: %+v", fa.triedFallbacks)
	}
	if len(fb.triedFallbacks["fb"]["w"]) != 0 {
		t.Fatalf("fb must not inherit fa's tried chain: %+v", fb.triedFallbacks)
	}
	if _, ok := readProposal(dir, "fa"); ok {
		t.Fatal("an adopted proposal must be cleared")
	}
	if _, ok := readProposal(dir, "fb"); !ok {
		t.Fatal("an unapproved proposal must stay pending")
	}
}

// Approving a task with no pending proposal is an ERROR, never a silent
// continue: otherwise the operator believes the agent was swapped while the run
// burns another whole budget cycle on the same configuration (spec §2.2).
func TestApproveFallbackUnknownTaskErrors(t *testing.T) {
	bindFallbackRoles(t)
	dir := t.TempDir()
	if err := writeProposal(dir, "fa", FallbackProposal{Task: "ta", Seat: "w", FromRole: "primary", ToRole: "backup"}); err != nil {
		t.Fatal(err)
	}

	opts, err := (Options{Dir: dir, ApproveFallback: []string{"ta", "nope"}}).applyApprovedFallback()
	if err == nil {
		t.Fatal("approving a task with no pending proposal must error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("the error must name the task that has no proposal: %v", err)
	}
	// Nothing may be half-applied: the run does not start, so no proposal is
	// consumed and no override is installed.
	if _, ok := readProposal(dir, "fa"); !ok {
		t.Fatal("a rejected approval must not consume the other proposals")
	}
	fa := opts
	fa.Feature = "fa"
	if got := fa.kind("w"); got != "claude-code" {
		t.Fatalf("a rejected approval must install no override, got %q", got)
	}
}

// Without --approve-fallback the pending proposals are left untouched.
func TestNoApproveLeavesEveryProposalPending(t *testing.T) {
	bindFallbackRoles(t)
	dir := t.TempDir()
	if err := writeProposal(dir, historyScopeAll, FallbackProposal{Task: "t1", Seat: "w", ToRole: "backup"}); err != nil {
		t.Fatal(err)
	}
	opts, err := (Options{Dir: dir}).applyApprovedFallback()
	if err != nil {
		t.Fatal(err)
	}
	if got := opts.kind("w"); got != "claude-code" {
		t.Fatalf("without approval the seat keeps its role kind, got %q", got)
	}
	if _, ok := readProposal(dir, historyScopeAll); !ok {
		t.Fatal("an unapproved proposal must stay pending")
	}
}

// The legacy single-file path is never read again, and adopting a proposal
// deletes it in passing so it cannot linger on disk forever (spec §2.1).
func TestLegacyProposalPathIsNeitherReadNorKept(t *testing.T) {
	bindFallbackRoles(t)
	dir := t.TempDir()
	legacy := filepath.Join(dir, ".pact", "orchestrate", "fallback-proposal.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"task":"old","seat":"w","toRole":"backup"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadProposals(dir); len(got) != 0 {
		t.Fatalf("the legacy single-file proposal must not be read: %+v", got)
	}
	if err := writeProposal(dir, historyScopeAll, FallbackProposal{Task: "t1", Seat: "w", FromRole: "primary", ToRole: "backup"}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Options{Dir: dir, ApproveFallback: []string{"t1"}}).applyApprovedFallback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("the legacy path must be swept away, stat err = %v", err)
	}
}

// Serial has exactly one scope, so an approval named on the command line takes
// effect even when the proposal was filed under a different feature filter (the
// dashboard's approve resumes without --feature). Otherwise the approval would
// be accepted and then silently not applied — the very failure §2.2 exists for.
func TestApproveFallbackAliasesIntoTheSerialRunScope(t *testing.T) {
	bindFallbackRoles(t)
	dir := t.TempDir()
	if err := writeProposal(dir, "f", FallbackProposal{Task: "t1", Seat: "w", FromRole: "primary", ToRole: "backup"}); err != nil {
		t.Fatal(err)
	}
	// Unfiltered serial run (scope "all") approving a proposal filed under "f".
	opts, err := (Options{Dir: dir, ApproveFallback: []string{"t1"}}).applyApprovedFallback(historyScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if got := opts.kind("w"); got != "opencode" {
		t.Fatalf("the serial run's own scope must see the approval, got %q", got)
	}
}
