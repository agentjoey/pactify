package pact

import (
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
		TaskID: "T1", Feature: "F", Payload: map[string]any{"owner": "opencode", "reviewer": "claude-opus", "branch": "feat/x", "spec": "s"}})
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

func TestAssignCreatesTask(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	if err := Assign("T1", "F", "feat/x", "opencode", "claude-opus", ".pact/tasks/T1.md", nil); err != nil {
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
	if err := p.Assign("T1", "F", "feat/x", "opencode", "claude-opus", "", nil); err != nil {
		t.Fatal(err)
	}
	evs, _ := event.ReadAll(filepath.Join(dir, ".pact", "log.jsonl"))
	var spec string
	for _, e := range evs {
		if e.EventType == "assign" {
			spec, _ = e.Payload["spec"].(string)
		}
	}
	if spec != ".pact/tasks/T1.md" {
		t.Fatalf("default spec = %q, want .pact/tasks/T1.md (repo-relative)", spec)
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
	// THEN T1 (no deps) and T2 (deps T1) are assigned to the same worker.
	orch := At(dir).As("claude-opus")
	if err := orch.Assign("T1", "F", "feat/x", "opencode", "claude-opus", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := orch.Assign("T2", "F", "feat/x", "opencode", "claude-opus", "", []string{"T1"}); err != nil {
		t.Fatal(err)
	}
	// T2 checkpoint must now fail at the checkpoint gate — T1 is not accepted.
	os.WriteFile(filepath.Join(dir, "impl.txt"), []byte("c"), 0o644)
	if err := At(dir).As("opencode").Checkpoint("T2", "ok"); err == nil {
		t.Fatal("checkpoint of T2 must be blocked by unaccepted dep T1")
	} else if !strings.Contains(err.Error(), "blocked by unaccepted dep T1") {
		t.Fatalf("error %q must name the blocking dep", err)
	}
	// Once T1 flows through to accepted, T2 checkpoint succeeds.
	if err := At(dir).As("opencode").Checkpoint("T1", "ok"); err != nil {
		t.Fatalf("T1 checkpoint: %v", err)
	}
	if err := At(dir).As("claude-opus").Accept("T1"); err != nil {
		t.Fatalf("T1 accept: %v", err)
	}
	if err := At(dir).As("opencode").Checkpoint("T2", "ok"); err != nil {
		t.Fatalf("T2 checkpoint after T1 accepted must succeed: %v", err)
	}
}

func TestAssignRejectsOwnerEqualsReviewer(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	if err := Assign("T1", "F", "b", "opencode", "opencode", "s", nil); err == nil {
		t.Fatal("owner==reviewer must be rejected")
	}
}

func TestAssignRejectsDuplicateTaskID(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	Assign("T1", "A", "b", "opencode", "claude-opus", "s", nil)
	if err := Assign("T1", "B", "b", "opencode", "claude-opus", "s", nil); err == nil {
		t.Fatal("duplicate task id must be rejected")
	}
}

func toAssigned(t *testing.T) {
	t.Helper()
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	Assign("T1", "F", "feat/x", "opencode", "claude-opus", ".pact/tasks/T1.md", nil)
	t.Setenv("PACT_AGENT_ID", "opencode")
	Join("opencode", "worker")
}

func TestCheckpointByOwnerSetsAwaitingReviewAndCommits(t *testing.T) {
	newRepo(t)
	toAssigned(t)
	os.WriteFile("impl.txt", []byte("code"), 0o644)
	if err := Checkpoint("T1", "tests green"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(".pact/STATE.yml")
	if !strings.Contains(string(b), "status: awaiting_review") || !strings.Contains(string(b), "evidence: tests green") {
		t.Fatalf("state: %s", b)
	}
	if out, _ := exec.Command("git", "status", "--porcelain").Output(); strings.TrimSpace(string(out)) != "" {
		t.Fatalf("tree not clean after checkpoint: %s", out)
	}
}

func TestCheckpointByNonOwnerRejected(t *testing.T) {
	newRepo(t)
	toAssigned(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Checkpoint("T1", "x"); err == nil {
		t.Fatal("non-owner checkpoint must be rejected")
	}
}

func TestCheckpointRequiresEvidence(t *testing.T) {
	newRepo(t)
	toAssigned(t)
	if err := Checkpoint("T1", ""); err == nil {
		t.Fatal("checkpoint must require evidence")
	}
}

func toAwaiting(t *testing.T) {
	t.Helper()
	toAssigned(t)
	os.WriteFile("impl.txt", []byte("c"), 0o644)
	Checkpoint("T1", "ok")
}

func TestAcceptByReviewer(t *testing.T) {
	newRepo(t)
	toAwaiting(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Accept("T1"); err != nil {
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
	if err := Accept("T1"); err == nil {
		t.Fatal("worker self-accept must be rejected")
	}
}

func TestAcceptRequiresAwaitingReview(t *testing.T) {
	newRepo(t)
	toAssigned(t) // T1 is assigned, not awaiting_review
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Accept("T1"); err == nil {
		t.Fatal("accept must require awaiting_review")
	}
}

func TestChangesSendsBack(t *testing.T) {
	newRepo(t)
	toAwaiting(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Changes("T1", "fix lint"); err != nil {
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
	toAwaiting(t) // T1 awaiting_review, not accepted
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Merge("F"); err == nil {
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
	Accept("T1")
	if err := Merge("F"); err != nil {
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
	Assign("T1", "F", base, "opencode", "claude-opus", ".pact/tasks/T1.md", nil)

	t.Setenv("PACT_AGENT_ID", "opencode")
	os.WriteFile("impl.txt", []byte("c"), 0o644)
	if err := Checkpoint("T1", "ok"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Accept("T1"); err != nil {
		t.Fatal(err)
	}
	if err := Merge("F"); err != nil {
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
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"})
	Assign("T1", "F", "feat/missing", "opencode", "claude-opus", ".pact/tasks/T1.md", nil)

	t.Setenv("PACT_AGENT_ID", "opencode")
	os.WriteFile("impl.txt", []byte("c"), 0o644)
	if err := Checkpoint("T1", "ok"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := Accept("T1"); err != nil {
		t.Fatal(err)
	}

	if err := Merge("F"); err == nil {
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
	Assign("A1", "fa", "feat/a", "opencode", "claude-opus", ".pact/tasks/A1.md", nil) // first feature
	Assign("B1", "fb", "feat/b", "opencode", "claude-opus", ".pact/tasks/B1.md", nil)
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
	Assign("T1", "F", "b", "opencode", "claude-opus", "s", nil)
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
