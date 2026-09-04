package orchestrate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// gateFlipExec succeeds until flip is set, then fails. The per-task verify and
// the feature's merge-time hard gate run the SAME command from the SAME dir, so
// the only way to fail just the hard gate is to flip after the task is accepted.
type gateFlipExec struct {
	mu   sync.Mutex
	fail bool
}

func (e *gateFlipExec) Run(_ context.Context, _, _ string, _ map[string]string) (int, string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fail {
		return 1, "FAIL: hard gate", nil
	}
	return 0, "ok", nil
}

func (e *gateFlipExec) flip() { e.mu.Lock(); e.fail = true; e.mu.Unlock() }

// flipRunner drives the pact cycle like parFakeRunner and flips the exec to
// failing once the task is accepted — i.e. exactly between the task gate and the
// merge-time hard gate.
type flipRunner struct {
	t    *testing.T
	exec *gateFlipExec
}

func (f flipRunner) Run(_ context.Context, lc LaunchContext) error {
	task := taskIDFromBrief(lc.Briefing)
	if task == "" {
		f.t.Fatalf("no task id in brief:\n%s", lc.Briefing)
	}
	if isWorker(lc.Briefing) {
		return pact.At(lc.RepoDir).As(lc.Seat).Checkpoint(task, "evidence: tests pass")
	}
	if err := pact.At(lc.RepoDir).As(lc.Seat).Accept(task); err != nil {
		return err
	}
	f.exec.flip()
	return nil
}

type capturingNotifier struct {
	mu   sync.Mutex
	msgs []string
}

func (n *capturingNotifier) Notify(m string) { n.mu.Lock(); n.msgs = append(n.msgs, m); n.mu.Unlock() }
func (n *capturingNotifier) all() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.msgs...)
}

// A parallel feature whose merge-time hard gate fails escalates through
// mergeFromWorktree, which sets o.Dir = worktreeDir. With RuntimeDir unset that
// makes runtimeDir() the WORKTREE — and settle removes that worktree moments
// later, so the record the operator is told to read is deleted with it.
func TestRunParallel_GateFailureEscalationSurvivesWorktreeRemoval(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")

	ex := &gateFlipExec{}
	notes := &capturingNotifier{}
	popts := ParallelOptions{
		Options: Options{
			Dir:          dir,
			Th:           Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50},
			Run:          flipRunner{t: t, exec: ex},
			Exec:         ex,
			Notify:       notes,
			Now:          func() string { return "20260614-000000" },
			SeatKind:     func(string) string { return "claude-code" },
			Orchestrator: "orch",
		},
		MaxConcurrency: 2,
		WorktreeRoot:   filepath.Join(t.TempDir(), "wt"),
	}
	_ = RunParallel(context.Background(), popts)

	// The operator was told something paused.
	msgs := notes.all()
	var paused string
	for _, m := range msgs {
		if strings.Contains(m, "orchestrate paused") {
			paused = m
		}
	}
	if paused == "" {
		t.Fatalf("a failed hard gate must notify, got %v", msgs)
	}

	// ...and the record it points at must still exist.
	idx := strings.LastIndex(paused, "see ")
	if idx < 0 {
		t.Fatalf("notification must name the escalation record: %q", paused)
	}
	recorded := strings.TrimSpace(paused[idx+len("see "):])
	if _, err := os.Stat(recorded); err != nil {
		t.Errorf("the escalation record the operator was pointed at is gone: %s (%v)", recorded, err)
	}

	// It belongs in the primary tree's runtime dir, which outlives the run.
	entries, _ := os.ReadDir(filepath.Join(dir, ".pact", "orchestrate"))
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "escalation-") {
			found = true
		}
	}
	if !found {
		t.Errorf("no escalation record under the primary tree: %v", names(entries))
	}
}

func names(es []os.DirEntry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// mergeFromWorktree returns `o.escalate(...)` on a failed hard gate — and
// escalate returns nil when the record is written. settle reads that nil as
// "merged" and stamps the feature done, so the aggregated view claims a feature
// shipped when it was actually paused for a human and never merged.
func TestRunParallel_FailedHardGateIsNotReportedAsShipped(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")

	ex := &gateFlipExec{}
	popts := ParallelOptions{
		Options: Options{
			Dir:          dir,
			Th:           Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50},
			Run:          flipRunner{t: t, exec: ex},
			Exec:         ex,
			Notify:       &capturingNotifier{},
			Now:          func() string { return "20260614-000000" },
			SeatKind:     func(string) string { return "claude-code" },
			Orchestrator: "orch",
		},
		MaxConcurrency: 2,
		WorktreeRoot:   filepath.Join(t.TempDir(), "wt"),
	}
	_ = RunParallel(context.Background(), popts)

	b, err := os.ReadFile(filepath.Join(dir, ".pact", "orchestrate", "parallel", "fa.json"))
	if err != nil {
		t.Fatalf("read feature status: %v", err)
	}
	if strings.Contains(string(b), `"done":true`) {
		t.Errorf("feature was escalated on a failed hard gate and never merged, but the aggregated status says done:\n%s", b)
	}
	// And the ledger must not show it shipped either.
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range st.Features {
		if f.ID == "fa" && f.Status == "shipped" {
			t.Errorf("feature fa is marked shipped in the ledger despite a failed hard gate")
		}
	}
}

// The backlog item: a merge that fails after every task is accepted leaves the
// feature unmerged and needs a human. The serial path pages one; the parallel
// path used to only record the error into dispatchErr — no escalation record, no
// notification, so a run could end with a silently unmerged feature.
func TestRunParallel_MergeFailureEscalatesAndNotifies(t *testing.T) {
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "assign fa")

	notes := &capturingNotifier{}
	popts := ParallelOptions{
		Options: Options{
			Dir:          dir,
			Th:           Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50},
			Run:          parFakeRunner{t: t},
			Exec:         &okExec{},
			Notify:       notes,
			Now:          func() string { return "20260614-000000" },
			SeatKind:     func(string) string { return "claude-code" },
			Orchestrator: "orch",
			mergeWorktree: func(context.Context, string, string) (bool, error) {
				return false, errMergeBoom
			},
		},
		MaxConcurrency: 2,
		WorktreeRoot:   filepath.Join(t.TempDir(), "wt"),
	}
	err := RunParallel(context.Background(), popts)

	if err == nil {
		t.Fatal("a failed merge must still surface as the run's error")
	}
	var paused string
	for _, m := range notes.all() {
		if strings.Contains(m, "orchestrate paused") {
			paused = m
		}
	}
	if paused == "" {
		t.Fatalf("a failed merge must notify a human, got %v", notes.all())
	}
	if !strings.Contains(paused, "merge failed") {
		t.Errorf("notification must say what happened, got %q", paused)
	}

	entries, _ := os.ReadDir(filepath.Join(dir, ".pact", "orchestrate"))
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "escalation-") {
			found = true
		}
	}
	if !found {
		t.Errorf("no escalation record for the failed merge: %v", names(entries))
	}

	b, _ := os.ReadFile(filepath.Join(dir, ".pact", "orchestrate", "parallel", "fa.json"))
	if strings.Contains(string(b), `"done":true`) {
		t.Errorf("an unmerged feature must not be reported done:\n%s", b)
	}
}

var errMergeBoom = errors.New("merge conflict in .pact/log.jsonl")
