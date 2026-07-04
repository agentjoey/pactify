package pact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// quorumRepo scaffolds a repo with an orchestrator, a worker, and three reviewer
// seats, assigns a quorum task (reviewers a,b,c; quorum 2), and checkpoints it so
// it sits awaiting_review. It returns the repo dir.
func quorumRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	repo := newLockRepo(t)
	p := At(repo)
	if err := p.Init("p", []string{
		"orch:orchestrator:CLAUDE.md", "w:worker:AGENTS.md",
		"a:reviewer:A.md", "b:reviewer:B.md", "c:reviewer:C.md",
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.As("orch").AssignQuorum("t1", "f", "feat/x", "w", []string{"a", "b", "c"}, 2, ".pact/tasks/t1.md", nil); err != nil {
		t.Fatal(err)
	}
	wk := At(repo).As("w")
	if err := wk.Join("w", "worker"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("x\n"), 0o644)
	if err := wk.Checkpoint("t1", "ok"); err != nil {
		t.Fatal(err)
	}
	return repo
}

func statusOf(t *testing.T, repo, task string) string {
	t.Helper()
	st, err := At(repo).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range st.Features {
		for _, tk := range f.Tasks {
			if tk.ID == task {
				return tk.Status
			}
		}
	}
	t.Fatalf("task %s not found", task)
	return ""
}

// A configured reviewer may accept a quorum task; below quorum it stays
// awaiting_review, and the second distinct reviewer meets quorum → accepted.
func TestQuorumEngineValidReviewerAccepts(t *testing.T) {
	repo := quorumRepo(t)
	if err := At(repo).As("a").Accept("t1"); err != nil {
		t.Fatalf("reviewer a accept: %v", err)
	}
	if got := statusOf(t, repo, "t1"); got != "awaiting_review" {
		t.Fatalf("after 1/2 accept want awaiting_review, got %q", got)
	}
	if err := At(repo).As("b").Accept("t1"); err != nil {
		t.Fatalf("reviewer b accept: %v", err)
	}
	if got := statusOf(t, repo, "t1"); got != "accepted" {
		t.Fatalf("after 2/2 accept want accepted, got %q", got)
	}
}

// A seat NOT in the reviewer set may not accept — the single-reviewer guard,
// generalized to the quorum set.
func TestQuorumEngineNonReviewerRejected(t *testing.T) {
	repo := quorumRepo(t)
	err := At(repo).As("orch").Accept("t1") // orch is not among reviewers a,b,c
	if err == nil {
		t.Fatal("a non-reviewer seat must not accept a quorum task")
	}
	if !strings.Contains(err.Error(), "reviewer") || !strings.Contains(err.Error(), "orch") {
		t.Fatalf("error must name the rejected seat and the reviewer requirement, got: %v", err)
	}
}

// The sacred rule survives quorum: the worker (task owner) may never self-accept,
// even though the task is awaiting_review. The owner is barred from the reviewer
// set at assign time, so the accept guard rejects it.
func TestQuorumEngineWorkerSelfAcceptRejected(t *testing.T) {
	repo := quorumRepo(t)
	err := At(repo).As("w").Accept("t1")
	if err == nil {
		t.Fatal("worker self-accept must be rejected under quorum")
	}
	if got := statusOf(t, repo, "t1"); got == "accepted" {
		t.Fatal("worker self-accept must not have accepted the task")
	}
}

// assign must reject an owner that also appears in the reviewer set (separation of
// duties generalized), and a quorum that exceeds the reviewer count.
func TestQuorumAssignValidation(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	repo := newLockRepo(t)
	p := At(repo)
	if err := p.Init("p", []string{"orch:orchestrator:CLAUDE.md", "w:worker:AGENTS.md", "a:reviewer:A.md"}); err != nil {
		t.Fatal(err)
	}
	// owner in reviewer set → separation-of-duties error.
	if err := p.As("orch").AssignQuorum("t1", "f", "feat/x", "w", []string{"a", "w"}, 2, "", nil); err == nil {
		t.Fatal("owner appearing in reviewers must be rejected")
	}
	// quorum > #reviewers → unattainable.
	if err := p.As("orch").AssignQuorum("t2", "f", "feat/x", "w", []string{"a"}, 2, "", nil); err == nil {
		t.Fatal("quorum exceeding reviewer count must be rejected")
	}
	// duplicate reviewers rejected.
	if err := p.As("orch").AssignQuorum("t3", "f", "feat/x", "w", []string{"a", "a"}, 2, "", nil); err == nil {
		t.Fatal("duplicate reviewers must be rejected")
	}
}

// GOLDEN: the legacy single-reviewer accept/changes path is byte-identical — the
// assign event carries the single `reviewer` key (no reviewers/quorum), only the
// named reviewer may act, and one accept accepts. This is the regression guard that
// the quorum generalization did not disturb the default path.
func TestSingleReviewerEngineGolden(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	repo := newLockRepo(t)
	p := At(repo)
	if err := p.Init("p", []string{"orch:orchestrator,reviewer:CLAUDE.md", "w:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	if err := p.As("orch").Assign("t1", "f", "feat/x", "w", "orch", ".pact/tasks/t1.md", nil); err != nil {
		t.Fatal(err)
	}
	wk := At(repo).As("w")
	if err := wk.Join("w", "worker"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("x\n"), 0o644)
	if err := wk.Checkpoint("t1", "ok"); err != nil {
		t.Fatal(err)
	}

	// The assign event must carry the classic single-reviewer payload shape.
	log, err := p.LogText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log, `"reviewer":"orch"`) {
		t.Fatalf("legacy assign must record single reviewer key; log:\n%s", log)
	}
	if strings.Contains(log, `"reviewers"`) || strings.Contains(log, `"quorum"`) {
		t.Fatalf("legacy assign must NOT record reviewers/quorum keys; log:\n%s", log)
	}

	// A non-reviewer cannot act; the named reviewer accepts and the task accepts.
	if err := wk.Accept("t1"); err == nil {
		t.Fatal("worker self-accept must be rejected (legacy path)")
	}
	if err := p.As("orch").Accept("t1"); err != nil {
		t.Fatalf("named reviewer accept: %v", err)
	}
	if got := statusOf(t, repo, "t1"); got != "accepted" {
		t.Fatalf("legacy single accept want accepted, got %q", got)
	}
}
