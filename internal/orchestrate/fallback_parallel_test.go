package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// parEnvFailRunner fails every stint without delivering anything — the env-class
// failure a fallback profile is supposed to rescue.
type parEnvFailRunner struct{ err error }

func (r parEnvFailRunner) Run(context.Context, LaunchContext) error { return r.err }

// kindRecordingRunner drives the pact engine like parFakeRunner while recording
// the KIND each task's worker was launched under — the observable effect of an
// approved fallback.
type kindRecordingRunner struct {
	t     *testing.T
	mu    sync.Mutex
	kinds map[string]string // task -> kind the worker launched with
}

func (r *kindRecordingRunner) Run(_ context.Context, lc LaunchContext) error {
	task := taskIDFromBrief(lc.Briefing)
	if task == "" {
		r.t.Fatalf("no task id in brief:\n%s", lc.Briefing)
	}
	if isWorker(lc.Briefing) {
		r.mu.Lock()
		r.kinds[task] = lc.Kind
		r.mu.Unlock()
		return pact.At(lc.RepoDir).As(lc.Seat).Checkpoint(task, "evidence: tests pass")
	}
	return pact.At(lc.RepoDir).As(lc.Seat).Accept(task)
}

func (r *kindRecordingRunner) kindOf(task string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.kinds[task]
}

// twoFeatureProject seeds fa/ta and fb/tb, both owned by seat `w`, committed to
// base so each feature's worktree inherits them.
func twoFeatureProject(t *testing.T) string {
	t.Helper()
	dir := newProject(t)
	writeSpec(t, dir, "ta", "true")
	writeSpec(t, dir, "tb", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	assignNoCheckout(t, dir, "tb", "fb", "feat-b", filepath.Join(".pact", "tasks", "tb.md"))
	gitCommitAll(t, dir, "assign fa+fb")
	return dir
}

func parOpts(dir string, run Runner, notify Notifier, conc int, t *testing.T) ParallelOptions {
	return ParallelOptions{
		Options: Options{
			Dir:          dir,
			Th:           Thresholds{MaxRework: 3, MaxFails: 1, MaxIters: 50},
			Run:          run,
			Exec:         &okExec{},
			Notify:       notify,
			Now:          func() string { return "20260614-000000" },
			Orchestrator: "orch",
			// Only the orchestrator seat gets an explicit kind: seat `w` must
			// resolve through the role layer, which is what an approved fallback
			// overrides.
			SeatKind: func(s string) string {
				if s == "orch" {
					return "claude-code"
				}
				return ""
			},
		},
		MaxConcurrency: conc,
		WorktreeRoot:   filepath.Join(t.TempDir(), "wt"),
	}
}

// Two features failing env-class at once each write their OWN proposal, both in
// the PRIMARY tree (the worktrees are gone by the time anyone reads them).
func TestRunParallelWritesOneProposalPerFeatureInPrimaryTree(t *testing.T) {
	bindFallbackRoles(t)
	dir := twoFeatureProject(t)
	notify := &syncNotify{}

	popts := parOpts(dir, parEnvFailRunner{context.DeadlineExceeded}, notify, 2, t)
	if err := RunParallel(context.Background(), popts); err != nil {
		t.Fatalf("RunParallel: %v", err)
	}

	for scope, task := range map[string]string{"fa": "ta", "fb": "tb"} {
		p, ok := readProposal(dir, scope)
		if !ok {
			t.Fatalf("feature %s must propose a fallback in the primary tree; notify=%s", scope, notify.all())
		}
		if p.Task != task || p.Seat != "w" || p.ToRole != "backup" {
			t.Fatalf("feature %s proposal = %+v", scope, p)
		}
	}
	if !strings.Contains(notify.all(), "orchestrate paused") {
		t.Fatalf("both features must pause: %s", notify.all())
	}
}

// The escalation markdown a parallel feature writes must survive its worktree's
// teardown: the notification's `see <path>` has to point at a file that exists.
func TestRunParallelEscalationRecordSurvivesWorktreeTeardown(t *testing.T) {
	bindFallbackRoles(t)
	dir := twoFeatureProject(t)
	notify := &syncNotify{}

	if err := RunParallel(context.Background(), parOpts(dir, parEnvFailRunner{context.DeadlineExceeded}, notify, 2, t)); err != nil {
		t.Fatalf("RunParallel: %v", err)
	}

	msgs := notify.all()
	var paths []string
	for _, line := range strings.Split(msgs, "\n") {
		if i := strings.Index(line, " — see "); i >= 0 {
			paths = append(paths, strings.TrimSpace(line[i+len(" — see "):]))
		}
	}
	if len(paths) == 0 {
		t.Fatalf("no escalation record was announced:\n%s", msgs)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("the announced escalation record does not exist (worktree teardown ate it): %s (%v)", p, err)
		}
		if !strings.HasPrefix(p, dir+string(os.PathSeparator)) {
			t.Fatalf("the escalation record must land in the primary tree, got %s", p)
		}
	}
	if got := readEscalations(t, dir); !strings.Contains(got, "failure limit exceeded") {
		t.Fatalf("primary tree has no readable escalation record:\n%s", got)
	}
}

// --approve-fallback used to be a no-op under --max-concurrency > 1 (spec §1(2)).
// Approving ONE feature's task must swap that feature's agent and leave the
// other feature — which shares the very same seat — on its bound role.
func TestRunParallelAdoptsOnlyTheApprovedFeaturesFallback(t *testing.T) {
	bindFallbackRoles(t)
	dir := twoFeatureProject(t)
	for scope, task := range map[string]string{"fa": "ta", "fb": "tb"} {
		if err := writeProposal(dir, scope, FallbackProposal{
			Task: task, Seat: "w", FromRole: "primary", ToRole: "backup", Tried: []string{"backup"},
		}); err != nil {
			t.Fatal(err)
		}
	}

	run := &kindRecordingRunner{t: t, kinds: map[string]string{}}
	popts := parOpts(dir, run, StdoutNotifier{}, 2, t)
	popts.Th = Thresholds{MaxRework: 3, MaxFails: 3, MaxIters: 50}
	popts.ApproveFallback = []string{"ta"}

	if err := RunParallel(context.Background(), popts); err != nil {
		t.Fatalf("RunParallel: %v", err)
	}

	if got := run.kindOf("ta"); got != "opencode" {
		t.Fatalf("the approved feature must launch seat w under the backup role, got %q", got)
	}
	if got := run.kindOf("tb"); got != "claude-code" {
		t.Fatalf("the feature that shares the seat but was never approved must keep its bound role, got %q", got)
	}
	if _, ok := readProposal(dir, "fb"); !ok {
		t.Fatal("the unapproved feature's proposal must stay pending")
	}
}

// A parallel run must refuse to start when an approval names no pending proposal.
func TestRunParallelRefusesUnknownApproval(t *testing.T) {
	bindFallbackRoles(t)
	dir := twoFeatureProject(t)
	run := &noLaunchRunner{}
	popts := parOpts(dir, run, StdoutNotifier{}, 2, t)
	popts.ApproveFallback = []string{"no-such-task"}
	if err := RunParallel(context.Background(), popts); err == nil {
		t.Fatal("a parallel run must refuse an approval that names no pending proposal")
	}
	if run.launched.Load() {
		t.Fatal("the run must refuse BEFORE launching any agent")
	}
	// It must also refuse BEFORE parking the primary tree.
	if _, err := os.Stat(parkMarkerPath(dir)); err == nil {
		t.Fatal("the run must fail before it parks the primary tree")
	}
}
