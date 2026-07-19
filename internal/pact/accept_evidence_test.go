package pact_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// acceptReadyRepo drives one task (t1, owner w, reviewer rev) to
// awaiting_review so accept variants can be exercised directly.
func acceptReadyRepo(t *testing.T) (repo string, orch *pact.Project) {
	t.Helper()
	repo, orch = depsRepo(t)
	if err := orch.Assign("t1", "f", "feat/x", "w", "rev", "", nil); err != nil {
		t.Fatal(err)
	}
	w := pact.At(repo).As("w")
	if err := w.Join("w", "worker"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "impl.txt"), []byte("x"), 0o644)
	if err := w.Checkpoint("t1", "owner evidence"); err != nil {
		t.Fatal(err)
	}
	return repo, orch
}

// Reviewer evidence (the verify run backing the verdict) is recorded on the
// accept event — closing the dogfood gap where the review side of the evidence
// chain could only live outside the protocol.
func TestAcceptEvidenceRecordedInLog(t *testing.T) {
	repo, orch := acceptReadyRepo(t)
	if err := orch.As("rev").AcceptEvidence("t1", "unittest 16/16 green"); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join(repo, ".pact/log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "unittest 16/16 green") {
		t.Fatalf("accept evidence must be recorded in log.jsonl:\n%s", log)
	}
	// Log-only discipline: reviewer evidence never enters the STATE projection
	// (the rendered STATE stays byte-aligned with evidence-free implementations).
	st, err := os.ReadFile(filepath.Join(repo, ".pact/STATE.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(st), "unittest 16/16 green") {
		t.Fatalf("accept evidence must NOT be projected into STATE.yml:\n%s", st)
	}
}

// Evidence-free accepts keep the historical payload: no evidence key at all,
// so existing logs and the bash oracle stay byte-identical.
func TestAcceptWithoutEvidenceKeepsPayloadBytes(t *testing.T) {
	repo, orch := acceptReadyRepo(t)
	if err := orch.As("rev").Accept("t1"); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join(repo, ".pact/log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(log), "\n") {
		if strings.Contains(line, `"accept"`) && strings.Contains(line, "evidence") {
			t.Fatalf("evidence-free accept must not emit an evidence key: %s", line)
		}
	}
}
