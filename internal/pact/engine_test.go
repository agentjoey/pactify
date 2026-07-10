package pact

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/gitx"
)

// newRepo makes a temp git repo, sets PACT_DIR + chdir, returns repo dir.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0o644)
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "base"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		c.CombinedOutput()
	}
	t.Setenv("PACT_DIR", filepath.Join(dir, ".pact"))
	t.Chdir(dir)
	return dir
}

func TestInitScaffoldsAndWritesInitEvent(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	err := Init("pactify", []string{
		"claude-opus:orchestrator,reviewer:CLAUDE.md",
		"opencode:worker:AGENTS.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(".pact/PROJECT.md"); err != nil {
		t.Fatal("PROJECT.md missing")
	}
	evs, _ := event.ReadAll(".pact/log.jsonl")
	if len(evs) != 1 || evs[0].EventType != "init" {
		t.Fatalf("want 1 init event, got %+v", evs)
	}
	pv, _ := evs[0].Payload["protocol_version"].(float64)
	if int(pv) != 1 {
		t.Fatalf("protocol_version = %v", evs[0].Payload["protocol_version"])
	}
	b, _ := os.ReadFile("AGENTS.md")
	if !strings.Contains(string(b), "PACT_AGENT_ID=opencode") {
		t.Fatal("AGENTS.md not baked")
	}
}

func TestInitFailsClosedWithoutAgentID(t *testing.T) {
	newRepo(t)
	os.Unsetenv("PACT_AGENT_ID")
	if err := Init("p", []string{"a:worker:A.md"}); err == nil {
		t.Fatal("Init must fail closed without PACT_AGENT_ID")
	}
}

func TestJoinAppendsEventAndChecksOutFeatureBranch(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	// seed an assign event directly (Assign verb arrives in Task 8)
	event.Append(".pact/log.jsonl", event.Event{AgentID: "claude-opus", Role: "orchestrator", EventType: "assign",
		TaskID: "t1", Feature: "f", Payload: map[string]any{"owner": "opencode", "reviewer": "claude-opus", "branch": "feat/x", "spec": "s"}})
	t.Setenv("PACT_AGENT_ID", "opencode")
	if err := Join("opencode", "worker"); err != nil {
		t.Fatal(err)
	}
	if b, _ := execBranch(); b != "feat/x" {
		t.Fatalf("join did not check out feat/x, on %q", b)
	}
}

func execBranch() (string, error) {
	c := exec.Command("git", "branch", "--show-current")
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err
}

func TestJoinFailsClosedWithoutAgentID(t *testing.T) {
	newRepo(t)
	os.Unsetenv("PACT_AGENT_ID")
	if err := Join("opencode", "worker"); err == nil {
		t.Fatal("Join must fail closed")
	}
}

func TestJoinPayloadIncludesProtocolVersion(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	t.Setenv("PACT_AGENT_ID", "opencode")
	if err := Join("opencode", "worker"); err != nil {
		t.Fatalf("join failed: %v", err)
	}
	evs, _ := event.ReadAll(".pact/log.jsonl")
	var joinPV int
	for _, e := range evs {
		if e.EventType == "join" {
			pv, _ := e.Payload["protocol_version"].(float64)
			joinPV = int(pv)
		}
	}
	if joinPV != 1 {
		t.Fatalf("join payload protocol_version = %d, want 1", joinPV)
	}
}

func TestJoinFailsClosedOnHigherProtocolMajor(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	b, _ := os.ReadFile(".pact/log.jsonl")
	out := strings.Replace(string(b), `"protocol_version":1`, `"protocol_version":99`, 1)
	os.WriteFile(".pact/log.jsonl", []byte(out), 0o644)
	if err := LogReplay(); err != nil {
		t.Fatalf("replay tampered log: %v", err)
	}
	t.Setenv("PACT_AGENT_ID", "opencode")
	if err := Join("opencode", "worker"); err == nil {
		t.Fatal("join must fail closed on higher major")
	} else if !strings.Contains(err.Error(), "upgrade pactify") {
		t.Fatalf("error %q must mention upgrade pactify", err)
	}
}

func TestJoinCompatibleWithLegacyNoProtocolVersion(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	b, _ := os.ReadFile(".pact/log.jsonl")
	// Remove protocol_version key entirely to simulate a pre-version log.
	out := strings.Replace(string(b), `,"protocol_version":1`, "", 1)
	os.WriteFile(".pact/log.jsonl", []byte(out), 0o644)
	if err := LogReplay(); err != nil {
		t.Fatalf("replay legacy log: %v", err)
	}
	t.Setenv("PACT_AGENT_ID", "opencode")
	if err := Join("opencode", "worker"); err != nil {
		t.Fatalf("join on legacy no-protocol_version log should succeed: %v", err)
	}
}

func TestAssignCreatesTask(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	if err := Assign("t1", "f", "feat/x", "opencode", "claude-opus", ".pact/tasks/t1.md", nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(".pact/STATE.yml")
	if !strings.Contains(string(b), "owner: opencode") || !strings.Contains(string(b), "status: assigned") {
		t.Fatalf("state: %s", b)
	}
}

func TestAssignDefaultSpecIsRepoRelative(t *testing.T) {
	dir := newRepo(t)
	// newRepo points PACT_DIR at an absolute path; clear it so the default spec
	// uses the repo-relative convention (the log must not carry a host path).
	os.Unsetenv("PACT_DIR")
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	p := At(dir).As("claude-opus")
	if err := p.Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	// Empty spec via an absolute-dir handle must default to a REPO-RELATIVE path
	// (no host-absolute leak into the shared log).
	if err := p.Assign("t1", "f", "feat/x", "opencode", "claude-opus", "", nil); err != nil {
		t.Fatal(err)
	}
	evs, _ := event.ReadAll(filepath.Join(dir, ".pact", "log.jsonl"))
	var spec string
	for _, e := range evs {
		if e.EventType == "assign" {
			spec, _ = e.Payload["spec"].(string)
		}
	}
	if spec != ".pact/tasks/t1.md" {
		t.Fatalf("default spec = %q, want .pact/tasks/t1.md (repo-relative)", spec)
	}
}

func TestCheckpointBlockedByUnacceptedDep(t *testing.T) {
	dir := newRepo(t)
	t.Setenv("PACT_AGENT_ID", "opencode")
	// Ordering hole: the worker joins FIRST (no tasks yet), so the join gate has
	// nothing to block on.
	if err := At(dir).As("claude-opus").Init("p", []string{
		"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md",
	}); err != nil {
		t.Fatal(err)
	}
	if err := At(dir).As("opencode").Join("opencode", "worker"); err != nil {
		t.Fatalf("early join: %v", err)
	}
	// THEN t1 (no deps) and t2 (deps t1) are assigned to the same worker.
	orch := At(dir).As("claude-opus")
	if err := orch.Assign("t1", "f", "feat/x", "opencode", "claude-opus", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("t2", "f", "feat/x", "opencode", "claude-opus", "", []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	// The worker checkpoints from its feature branch (a real worker checks it out at
	// join; the P3-1 guard refuses a checkpoint made on base when a branch is declared).
	if err := gitx.CheckoutOrCreate(dir, "feat/x"); err != nil {
		t.Fatalf("checkout feat/x: %v", err)
	}
	// t2 checkpoint must now fail at the checkpoint gate — t1 is not accepted.
	os.WriteFile(filepath.Join(dir, "impl.txt"), []byte("c"), 0o644)
	if err := At(dir).As("opencode").Checkpoint("t2", "ok"); err == nil {
		t.Fatal("checkpoint of t2 must be blocked by unaccepted dep t1")
	} else if !strings.Contains(err.Error(), "blocked by unaccepted dep t1") {
		t.Fatalf("error %q must name the blocking dep", err)
	}
	// Once t1 flows through to accepted, t2 checkpoint succeeds.
	if err := At(dir).As("opencode").Checkpoint("t1", "ok"); err != nil {
		t.Fatalf("t1 checkpoint: %v", err)
	}
	if err := At(dir).As("claude-opus").Accept("t1"); err != nil {
		t.Fatalf("t1 accept: %v", err)
	}
	if err := At(dir).As("opencode").Checkpoint("t2", "ok"); err != nil {
		t.Fatalf("t2 checkpoint after t1 accepted must succeed: %v", err)
	}
}

func TestAssignRejectsOwnerEqualsReviewer(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	if err := Assign("t1", "f", "b", "opencode", "opencode", "s", nil); err == nil {
		t.Fatal("owner==reviewer must be rejected")
	}
}

func TestAssignRejectsDuplicateTaskID(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	Assign("t1", "a", "b", "opencode", "claude-opus", "s", nil)
	if err := Assign("t1", "b", "b", "opencode", "claude-opus", "s", nil); err == nil {
		t.Fatal("duplicate task id must be rejected")
	}
}

func toAssigned(t *testing.T) {
	t.Helper()
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	Assign("t1", "f", "feat/x", "opencode", "claude-opus", ".pact/tasks/t1.md", nil)
	t.Setenv("PACT_AGENT_ID", "opencode")
	Join("opencode", "worker")
}

func TestCheckpointByOwnerSetsAwaitingReviewAndCommits(t *testing.T) {
	newRepo(t)
	toAssigned(t)
	os.WriteFile("impl.txt", []byte("code"), 0o644)
	if err := Checkpoint("t1", "tests green"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(".pact/STATE.yml")
	if !strings.Contains(string(b), "status: awaiting_review") || !strings.Contains(string(b), "evidence: tests green") {
		t.Fatalf("state: %s", b)
	}
	// Git-first write order: the WORK is committed before the checkpoint event is
	// appended, so the ledger files (.pact/) are dirtied AFTER the commit and stay
	// uncommitted until merge's "ledger before merge" sweep — only the work must
	// be clean here, and the ledger must not have ridden along in the work commit.
	out, _ := exec.Command("git", "status", "--porcelain").Output()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" && !strings.Contains(line, ".pact/") {
			t.Fatalf("work not committed by checkpoint: %s", out)
		}
	}
}

func TestCheckpointByNonOwnerRejected(t *testing.T) {
	newRepo(t)
	toAssigned(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Checkpoint("t1", "x"); err == nil {
		t.Fatal("non-owner checkpoint must be rejected")
	}
}

func TestCheckpointRequiresEvidence(t *testing.T) {
	newRepo(t)
	toAssigned(t)
	if err := Checkpoint("t1", ""); err == nil {
		t.Fatal("checkpoint must require evidence")
	}
}

func toAwaiting(t *testing.T) {
	t.Helper()
	toAssigned(t)
	os.WriteFile("impl.txt", []byte("c"), 0o644)
	Checkpoint("t1", "ok")
}

func TestAcceptByReviewer(t *testing.T) {
	newRepo(t)
	toAwaiting(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Accept("t1"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(".pact/STATE.yml")
	if !strings.Contains(string(b), "status: accepted") {
		t.Fatalf("state: %s", b)
	}
}

func TestAcceptByWorkerRejected(t *testing.T) {
	newRepo(t)
	toAwaiting(t)
	t.Setenv("PACT_AGENT_ID", "opencode")
	if err := Accept("t1"); err == nil {
		t.Fatal("worker self-accept must be rejected")
	}
}

func TestAcceptRequiresAwaitingReview(t *testing.T) {
	newRepo(t)
	toAssigned(t) // t1 is assigned, not awaiting_review
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Accept("t1"); err == nil {
		t.Fatal("accept must require awaiting_review")
	}
}

func TestChangesSendsBack(t *testing.T) {
	newRepo(t)
	toAwaiting(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Changes("t1", "fix lint"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(".pact/STATE.yml")
	if !strings.Contains(string(b), "status: changes_requested") {
		t.Fatalf("state: %s", b)
	}
}

func baseBranchFromLog() string {
	evs, _ := event.ReadAll(".pact/log.jsonl")
	for _, e := range evs {
		if e.EventType == "init" {
			if s, ok := e.Payload["base_branch"].(string); ok {
				return s
			}
		}
	}
	return ""
}

func TestMergeRejectedWhenNotAllAccepted(t *testing.T) {
	newRepo(t)
	toAwaiting(t) // t1 awaiting_review, not accepted
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Merge("f"); err == nil {
		t.Fatal("merge must be rejected when a task is not accepted")
	}
}

func TestMergeUnknownFeatureRejected(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	if err := Merge("NOPE"); err == nil {
		t.Fatal("merge of unknown feature must be rejected")
	}
}

func TestMergeSucceedsFeatureShipped(t *testing.T) {
	newRepo(t)
	toAwaiting(t)
	base := baseBranchFromLog()
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Accept("t1")
	if err := Merge("f"); err != nil {
		t.Fatal(err)
	}
	if b, _ := execBranch(); b != base {
		t.Fatalf("merge did not return to base %q, on %q", base, b)
	}
	b, _ := os.ReadFile(".pact/STATE.yml")
	if !strings.Contains(string(b), "status: shipped") {
		t.Fatalf("feature not shipped: %s", b)
	}
	if out, _ := exec.Command("git", "log", "--oneline").Output(); !strings.Contains(string(out), "Merge") {
		t.Fatalf("no merge commit: %s", out)
	}
}

// In-place work — a feature that declares the BASE branch (no separate feature
// branch) — ships without a git merge: the work already lives on base.
func TestMergeInPlaceOnBaseBranchShips(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	base, _ := execBranch() // the base branch — declaring it means "work in-place"
	Assign("t1", "f", base, "opencode", "claude-opus", ".pact/tasks/t1.md", nil)

	t.Setenv("PACT_AGENT_ID", "opencode")
	os.WriteFile("impl.txt", []byte("c"), 0o644)
	if err := Checkpoint("t1", "ok"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Accept("t1"); err != nil {
		t.Fatal(err)
	}
	if err := Merge("f"); err != nil {
		t.Fatalf("in-place merge on base should succeed, got %v", err)
	}
	if after, _ := execBranch(); after != base {
		t.Fatalf("in-place merge should stay on base %q, on %q", base, after)
	}
	b, _ := os.ReadFile(".pact/STATE.yml")
	if !strings.Contains(string(b), "status: shipped") {
		t.Fatalf("feature not shipped: %s", b)
	}
}

// A feature that declares its OWN (non-base) branch which was never created must
// NOT be recorded as shipped — the work never landed on that branch (the owner
// likely committed elsewhere, e.g. the join-wrong-branch bug). Merge must refuse,
// so pact's state can't run ahead of git.
func TestMergeRefusesMissingDeclaredBranch(t *testing.T) {
	dir := newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	Assign("t1", "f", "feat/missing", "opencode", "claude-opus", ".pact/tasks/t1.md", nil)

	t.Setenv("PACT_AGENT_ID", "opencode")
	// The owner commits ELSEWHERE (not on base — the P3-1 guard forbids that — and not
	// on the declared feat/missing): simulates the wrong-branch bug. The declared
	// branch still never gets created, so merge must refuse.
	if err := gitx.CheckoutOrCreate(dir, "feat/elsewhere"); err != nil {
		t.Fatalf("checkout feat/elsewhere: %v", err)
	}
	os.WriteFile("impl.txt", []byte("c"), 0o644)
	if err := Checkpoint("t1", "ok"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Accept("t1"); err != nil {
		t.Fatal(err)
	}

	if err := Merge("f"); err == nil {
		t.Fatal("merge recorded shipped despite the declared branch feat/missing never existing")
	}
	b, _ := os.ReadFile(".pact/STATE.yml")
	if strings.Contains(string(b), "status: shipped") {
		t.Fatalf("feature must not be shipped when its branch is missing:\n%s", b)
	}
}

// Join must NOT yank a seat that owns tasks in several features onto the FIRST
// feature's branch. The orchestrator checks out the specific task's branch before
// launching the worker; if the tree is already on a branch the seat owns a task
// in, join stays. (Old bug: join checked out the first owned feature's branch, so
// a multi-feature worker committed to the wrong branch.)
func TestJoinStaysOnSeatBranchNotFirstFeature(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	Assign("a1", "fa", "feat/a", "opencode", "claude-opus", ".pact/tasks/a1.md", nil) // first feature
	Assign("b1", "fb", "feat/b", "opencode", "claude-opus", ".pact/tasks/b1.md", nil)
	if err := gitx.CheckoutOrCreate(".", "feat/b"); err != nil { // orchestrator set THIS task's branch
		t.Fatal(err)
	}

	t.Setenv("PACT_AGENT_ID", "opencode")
	if err := Join("opencode", "worker"); err != nil {
		t.Fatal(err)
	}
	if cur, _ := execBranch(); cur != "feat/b" {
		t.Fatalf("join yanked the seat off its task branch to %q, want feat/b", cur)
	}
}

// config base-branch overrides the base pact captured at init — the fix for a
// project whose init recorded a feature branch as the base (so merges targeted the
// wrong branch and never reached the default).
func TestConfigBaseBranchOverridesInitBase(t *testing.T) {
	dir := newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})

	evs, _ := event.ReadAll(filepath.Join(dir, ".pact", "log.jsonl"))
	if _, explicit := baseBranch(evs); explicit {
		t.Fatal("init base should be implicit, not explicit")
	}
	if err := ConfigBaseBranch("main"); err != nil {
		t.Fatal(err)
	}
	evs, _ = event.ReadAll(filepath.Join(dir, ".pact", "log.jsonl"))
	if b, explicit := baseBranch(evs); b != "main" || !explicit {
		t.Fatalf("after override baseBranch = %q explicit=%v, want main/true", b, explicit)
	}
}

func TestStatusReturnsRenderedState(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	s, err := Status()
	if err != nil || !strings.Contains(s, "project: p") {
		t.Fatalf("status=%q err=%v", s, err)
	}
}

func TestValidatePassesOnConformantRepo(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	Assign("t1", "f", "b", "opencode", "claude-opus", "s", nil)
	if err := Validate(); err != nil {
		t.Fatalf("validate should pass: %v", err)
	}
}

func TestValidateFailsClosedOnHigherProtocolMajor(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	b, _ := os.ReadFile(".pact/log.jsonl")
	out := strings.Replace(string(b), `"protocol_version":1`, `"protocol_version":2`, 1)
	os.WriteFile(".pact/log.jsonl", []byte(out), 0o644)
	if err := Validate(); err == nil {
		t.Fatal("validate must fail closed on higher major")
	}
}

func TestValidateAcceptsDynamicJoinSeat(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	t.Setenv("PACT_AGENT_ID", "dynseat")
	if err := JoinKind("dynseat", "worker", "headless"); err != nil {
		t.Fatalf("dynamic join failed: %v", err)
	}
	if err := Validate(); err != nil {
		t.Fatalf("validate should accept dynamic join seat: %v", err)
	}
}

func TestValidateAcceptsAddedSeat(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	if err := AddSeat("newseat:worker:NEWSEAT.md"); err != nil {
		t.Fatalf("add-seat failed: %v", err)
	}
	t.Setenv("PACT_AGENT_ID", "newseat")
	if err := Join("newseat", "worker"); err != nil {
		t.Fatalf("added seat join failed: %v", err)
	}
	if err := Validate(); err != nil {
		t.Fatalf("validate should accept added seat: %v", err)
	}
}

func TestValidateStillRejectsUnknownSeat(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	b, _ := os.ReadFile(".pact/log.jsonl")
	out := strings.Replace(string(b), `"agent_id":"claude-opus"`, `"agent_id":"ghost"`, 1)
	os.WriteFile(".pact/log.jsonl", []byte(out), 0o644)
	// Regenerate STATE.yml from the tampered log so the STATE-drift check passes
	// and Validate must reach the roster check — otherwise this test would pass on
	// drift alone and prove nothing about unknown-seat rejection. "ghost" is the
	// init event's agent_id but never appears in any seats payload / add-seat /
	// join, so it is absent from st.Agents and only the roster check can reject it.
	if err := LogReplay(); err != nil {
		t.Fatalf("replay tampered log: %v", err)
	}
	if err := Validate(); err == nil {
		t.Fatal("validate must still reject unknown seat")
	}
}

func TestLogReplayRebuildsState(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	os.Remove(".pact/STATE.yml")
	if err := LogReplay(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(".pact/STATE.yml"); err != nil {
		t.Fatal("replay did not rebuild STATE.yml")
	}
}

func TestValidateReportsDriftWhenStateMissing(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	os.Remove(".pact/STATE.yml")
	if err := Validate(); err == nil {
		t.Fatal("validate must report drift when STATE.yml is missing (parity with bash)")
	}
}

// A repo that has deliberately committed .pact/ to git (this repo's own choice,
// to dogfood a full ledger audit history) must never sit dirty after a verb: a
// verb only auto-commits REAL file changes (checkpointLocked's HasChanges +
// CommitAll), never the ledger itself, so without this the ledger would be left
// modified-on-disk-but-uncommitted after every single call — which is exactly
// what let a coding-agent worker mistake the driver's own concurrent ledger
// writes for external corruption and "fix" them with `git restore` (2026-07-05
// dogfood P4), and what tripped RunSandbox's dirty-tree gate on nothing actually
// wrong (P5).
func TestCommitLedgerIfTrackedAutoCommitsWhenLedgerIsTracked(t *testing.T) {
	dir := newRepo(t)
	t.Setenv("PACT_AGENT_ID", "orch")
	if err := Init("p", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	// Deliberately track .pact/ — mirrors this repo's own choice.
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "track .pact")

	if err := At(".").As("orch").Assign("t1", "f", "feat/x", "w", "orch", ".pact/tasks/t1.md", nil); err != nil {
		t.Fatal(err)
	}
	if out := gitPorcelain(t, dir); out != "" {
		t.Fatalf("ledger left dirty after assign despite .pact/ being tracked:\n%s", out)
	}
	// A second verb must ALSO leave the tree clean (not just the first). Checkout
	// the declared feature branch first (checkpointLocked's base-write guard).
	runGit(t, dir, "checkout", "-q", "-b", "feat/x")
	if err := At(".").As("w").Checkpoint("t1", "evidence"); err != nil {
		t.Fatal(err)
	}
	if out := gitPorcelain(t, dir); out != "" {
		t.Fatalf("ledger left dirty after checkpoint despite .pact/ being tracked:\n%s", out)
	}
}

// The common/default case — .pact/ gitignored (or simply never committed) —
// must be entirely unaffected: pactify must never surprise a project by
// starting to `git add`/commit a ledger nobody asked it to track.
func TestCommitLedgerIfTrackedNoopWhenLedgerNeverTracked(t *testing.T) {
	dir := newRepo(t)
	t.Setenv("PACT_AGENT_ID", "orch")
	if err := Init("p", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	before := commitCount(t, dir)

	if err := At(".").As("orch").Assign("t1", "f", "feat/x", "w", "orch", ".pact/tasks/t1.md", nil); err != nil {
		t.Fatal(err)
	}
	if after := commitCount(t, dir); after != before {
		t.Fatalf("assign created a commit (%d -> %d) for a ledger nobody asked pactify to track", before, after)
	}
	// The ledger content is still on disk uncommitted — unchanged pre-existing
	// behavior for a never-tracked .pact/, not silently lost.
	if _, err := os.Stat(".pact/log.jsonl"); err != nil {
		t.Fatal("ledger file itself should still exist on disk")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func gitPorcelain(t *testing.T, dir string) string {
	t.Helper()
	c := exec.Command("git", "status", "--porcelain")
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func commitCount(t *testing.T, dir string) int {
	t.Helper()
	c := exec.Command("git", "rev-list", "--count", "HEAD")
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n)
	return n
}
