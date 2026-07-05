package orchestrate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// --- harness -----------------------------------------------------------------

// newProject builds a real git repo with a .pact project: an orchestrator/
// reviewer seat `orch` and a worker seat `w`. It returns the repo dir. Tasks are
// assigned by the caller. PACT_AGENT_ID is left set to `orch` (the seat that
// runs Init/Assign); fakeRunner uses As(seat) to act as other seats without
// touching env, so the engine's caller checks (checkpoint=owner, accept/changes=
// reviewer) are satisfied per-verb.
func newProject(t *testing.T) string {
	t.Helper()
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

	// Keep PACT_DIR unset so specs/log use the repo-relative convention.
	os.Unsetenv("PACT_DIR")
	t.Setenv("PACT_AGENT_ID", "orch")

	p := pact.At(dir).As("orch")
	if err := p.Init("proj", []string{
		"orch:orchestrator,reviewer:CLAUDE.md",
		"w:worker:AGENTS.md",
	}); err != nil {
		t.Fatalf("init: %v", err)
	}
	return dir
}

// writeSpec writes a task spec file with a verify line under .pact/tasks/.
func writeSpec(t *testing.T, dir, taskID, verify string) string {
	t.Helper()
	rel := filepath.Join(".pact", "tasks", taskID+".md")
	abs := filepath.Join(dir, rel)
	body := "# " + taskID + "\n\nverify: " + verify + "\n"
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return rel
}

// assign records a task assignment (owner=w, reviewer=orch) acting as orch and
// ensures the feature branch exists + is checked out, so the worker's checkpoint
// commits land on the branch the merge later integrates (this is what a real
// worker's `pactify join` would do at cold start).
func assign(t *testing.T, dir, taskID, feature, branch, spec string) {
	t.Helper()
	if err := pact.At(dir).As("orch").Assign(taskID, feature, branch, "w", "orch", spec, nil); err != nil {
		t.Fatalf("assign %s: %v", taskID, err)
	}
	c := exec.Command("git", "checkout", "-q", "-B", branch)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("checkout %s: %v %s", branch, err, out)
	}
}

// taskIDFromBrief extracts the task id from a briefing (briefs render `task `<id>“).
func taskIDFromBrief(brief string) string {
	// Both worker and reviewer briefs contain: working/reviewing task `<id>`
	i := strings.Index(brief, "task `")
	if i < 0 {
		return ""
	}
	rest := brief[i+len("task `"):]
	j := strings.Index(rest, "`")
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func isWorker(brief string) bool { return strings.Contains(brief, "pact worker") }

// fakeRunner drives the pact engine deterministically in response to briefings.
// worker brief → Checkpoint; reviewer brief → Changes for the first
// changesBeforeAccept rounds (per task), then Accept. It records call counts.
type fakeRunner struct {
	t                   *testing.T
	dir                 string
	changesBeforeAccept int            // reviewer says "changes" this many times before accepting
	reviewSeen          map[string]int // task -> reviewer invocations
	workerCalls         int
	reviewerCalls       int
	alwaysChanges       bool // reviewer never accepts (escalation test)
}

func newFakeRunner(t *testing.T, dir string) *fakeRunner {
	return &fakeRunner{t: t, dir: dir, reviewSeen: map[string]int{}}
}

func (f *fakeRunner) Run(ctx context.Context, lc LaunchContext) error {
	seatID, briefing := lc.Seat, lc.Briefing
	task := taskIDFromBrief(briefing)
	if task == "" {
		f.t.Fatalf("could not parse task id from brief:\n%s", briefing)
	}
	if isWorker(briefing) {
		f.workerCalls++
		// The worker checkpoints from the feature branch (the harness checks it
		// out at setup so checkpoint commits land there and the later merge has a
		// branch to integrate). We deliberately do NOT `join` here: a join would
		// move every task this seat owns to in_progress at once, and nextAction
		// only relaunches assigned/changes_requested tasks — so a second task
		// would strand. Real cold-start join is exercised in the engine tests.
		if err := pact.At(f.dir).As(seatID).Checkpoint(task, "evidence: tests pass"); err != nil {
			return err
		}
		return nil
	}
	// reviewer
	f.reviewerCalls++
	n := f.reviewSeen[task]
	f.reviewSeen[task] = n + 1
	if f.alwaysChanges || n < f.changesBeforeAccept {
		return pact.At(f.dir).As(seatID).Changes(task, "fix the edge case in round "+task)
	}
	return pact.At(f.dir).As(seatID).Accept(task)
}

// okExec is a cmdExec that always exits 0 (gate PASS).
type okExec struct{ calls int }

func (e *okExec) Run(ctx context.Context, dir, command string) (int, string, error) {
	e.calls++
	return 0, "ok", nil
}

// failExec is a cmdExec that always exits non-zero (gate FAIL).
type failExec struct{ calls int }

func (e *failExec) Run(ctx context.Context, dir, command string) (int, string, error) {
	e.calls++
	return 1, "FAIL: assertion failed", nil
}

// recNotify records escalation/preview messages.
type recNotify struct{ msgs []string }

func (n *recNotify) Notify(m string) { n.msgs = append(n.msgs, m) }

func fixedNow() string { return "20260613-120000" }

func featureStatus(t *testing.T, dir, feature string) string {
	t.Helper()
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	for _, f := range st.Features {
		if f.ID == feature {
			return f.Status
		}
	}
	return ""
}

func baseOpts(dir string, run Runner, exec cmdExec, notify Notifier) Options {
	return Options{
		Dir:      dir,
		Th:       Thresholds{MaxRework: 3, MaxFails: 2, MaxIters: 50},
		Run:      run,
		Exec:     exec,
		Notify:   notify,
		Now:      fixedNow,
		SeatKind: func(string) string { return "claude-code" },
	}
}

// --- cases -------------------------------------------------------------------

// (1) happy: two tasks → feature shipped.
// A --feature filter that matches nothing in the ledger this run actually sees
// must fail loudly, not silently proceed to ActDone (total=0, done=true) as if
// there were nothing to do. Silent success here is exactly how a sandbox
// worktree seeded from the wrong/stale ledger (or a typo'd --feature) got
// misdiagnosed as a healthy "nothing to do" during the 2026-07-05 dogfood (P5).
func TestLoopUnknownFeatureFailsLoudInsteadOfSilentDone(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := newFakeRunner(t, dir)
	exec := &okExec{}
	notify := &recNotify{}
	opts := baseOpts(dir, runner, exec, notify)
	opts.Feature = "does-not-exist"

	err := Run(context.Background(), opts)
	if err == nil {
		t.Fatal("Run should fail loud for a --feature that matches nothing, not silently report done")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should name the unmatched feature, got: %v", err)
	}
	// The real feature "f" must be untouched — this is a fail-fast guard, not a
	// partial run.
	if got := featureStatus(t, dir, "f"); got == "shipped" {
		t.Error("unrelated feature should not have been driven")
	}
}

func TestLoopHappyPathShipsFeature(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	s2 := writeSpec(t, dir, "t2", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)
	assign(t, dir, "t2", "f", "feat/x", s2)

	runner := newFakeRunner(t, dir)
	exec := &okExec{}
	notify := &recNotify{}
	if err := Run(context.Background(), baseOpts(dir, runner, exec, notify)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped", got)
	}
	if exec.calls == 0 {
		t.Fatal("hard gate was never run before merge")
	}
}

// A single-run orchestrate scaffolds the .pact/orchestrate/ gitignore so runtime
// files (per-task stream logs, status.json, escalation records) written during
// the run never pollute the user's repo nor get swept into an agent's
// `git add -A` during verify. The parallel path already does this via
// ensureUnionAttrs; the single-run path must too.
func TestLoopScaffoldsRuntimeGitignore(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	if err := Run(context.Background(), baseOpts(dir, newFakeRunner(t, dir), &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}

	rel, err := filepath.Rel(dir, StreamPath(dir, "t1"))
	if err != nil {
		t.Fatal(err)
	}
	c := exec.Command("git", "check-ignore", rel)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("runtime file %s not gitignored after single run (check-ignore: %v): %s", rel, err, out)
	}
}

// ensureRuntimeExcludedLocal ignores runtime artifacts via .git/info/exclude — a
// per-clone, NEVER-committed file — so it must create no commit (and thus never
// move base out from under a concurrent writer; see spec coordination-authority
// P0a, replacing the old committing ensureRuntimeIgnored / bcf9bf8 collision).
func TestEnsureRuntimeExcludedLocalNeverCommits(t *testing.T) {
	dir := newProject(t)
	before := headOf(t, dir)

	if err := ensureRuntimeExcludedLocal(dir); err != nil {
		t.Fatalf("ensureRuntimeExcludedLocal: %v", err)
	}

	// runtime artifacts are ignored...
	ci := exec.Command("git", "check-ignore", ".pact/orchestrate/streams/x.log")
	ci.Dir = dir
	if out, err := ci.CombinedOutput(); err != nil {
		t.Fatalf("runtime path not ignored after local exclude (%v): %s", err, out)
	}
	// ...but no commit was made — base is exactly where it was.
	if after := headOf(t, dir); after != before {
		t.Fatalf("ensureRuntimeExcludedLocal moved HEAD %s→%s; it must never write a tracked branch", before, after)
	}
}

// The serial driver must never commit a runtime-ignore chore to base. It used to,
// via ensureRuntimeIgnored (linx bcf9bf8), which moved base under a concurrent
// ff-merge. Runtime artifacts are now excluded locally. Spec: coordination-authority P0a.
func TestLoopDoesNotCommitIgnoreToBase(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	if err := Run(context.Background(), baseOpts(dir, newFakeRunner(t, dir), &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}

	lg := exec.Command("git", "log", "--all", "--oneline", "--grep", "ignore .pact/orchestrate")
	lg.Dir = dir
	out, _ := lg.CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("serial run committed a runtime-ignore chore to a tracked branch; want none:\n%s", out)
	}
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	c := exec.Command("git", "rev-parse", "HEAD")
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// (2) rework: reviewer requests changes once, then accepts → shipped, worker re-launched.
func TestLoopReworkThenShips(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := newFakeRunner(t, dir)
	runner.changesBeforeAccept = 1 // one changes round, then accept
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped", got)
	}
	if runner.workerCalls < 2 {
		t.Fatalf("worker launched %d times, want >=2 (initial + rework)", runner.workerCalls)
	}
}

// A worker that exits cleanly but never checkpoints (reports done yet delivers
// nothing — the opencode class) must escalate with a SPECIFIC cause, not a bare
// "failure limit exceeded". Verify must FAIL here: a passing verify now means
// the work is done and the driver rescues it with a checkpoint instead (see
// TestCleanExitNoCheckpointRescuedByVerify).
func TestEscalationNamesNoCheckpointCause(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "false")
	assign(t, dir, "t1", "f", "feat/x", s1)

	opts := baseOpts(dir, noCheckpointRunner{}, &failExec{}, &recNotify{})
	opts.Th.MaxFails = 2
	notify := opts.Notify.(*recNotify)
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	joined := strings.Join(notify.msgs, "\n")
	if !strings.Contains(joined, "no checkpoint") {
		t.Fatalf("escalation should name the no-checkpoint cause, got: %q", joined)
	}
	// And the escalation record carries it too.
	esc := filepath.Join(dir, ".pact", "orchestrate", "escalation-"+fixedNow()+".md")
	b, err := os.ReadFile(esc)
	if err != nil {
		t.Fatalf("escalation file missing: %v", err)
	}
	if !strings.Contains(string(b), "no checkpoint") {
		t.Fatalf("escalation record should name the no-checkpoint cause:\n%s", b)
	}
}

// noCheckpointRunner simulates a worker that runs to completion (no error) but
// never checkpoints — the task never reaches awaiting_review.
type noCheckpointRunner struct{}

func (noCheckpointRunner) Run(_ context.Context, _ LaunchContext) error { return nil }

// (3) escalation: reviewer always requests changes → rework limit → escalation file, no merge.
func TestLoopRebuffsEscalatesAtReworkLimit(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := newFakeRunner(t, dir)
	runner.alwaysChanges = true
	opts := baseOpts(dir, runner, &okExec{}, &recNotify{})
	opts.Th.MaxRework = 2
	notify := opts.Notify.(*recNotify)
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got == "shipped" {
		t.Fatal("feature shipped despite repeated changes_requested")
	}
	esc := filepath.Join(dir, ".pact", "orchestrate", "escalation-"+fixedNow()+".md")
	if _, err := os.Stat(esc); err != nil {
		t.Fatalf("escalation file missing: %v", err)
	}
	if len(notify.msgs) == 0 {
		t.Fatal("no escalation notification sent")
	}
}

// (4) hard gate intercept: all tasks accepted but gate fails → no merge, escalation.
func TestLoopHardGateBlocksMerge(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := newFakeRunner(t, dir) // accepts immediately
	exec := &failExec{}
	notify := &recNotify{}
	if err := Run(context.Background(), baseOpts(dir, runner, exec, notify)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := featureStatus(t, dir, "f"); got == "shipped" {
		t.Fatal("feature merged despite failing hard gate")
	}
	if exec.calls == 0 {
		t.Fatal("gate exec never invoked")
	}
	esc := filepath.Join(dir, ".pact", "orchestrate", "escalation-"+fixedNow()+".md")
	if _, err := os.Stat(esc); err != nil {
		t.Fatalf("escalation file missing after gate failure: %v", err)
	}
}

// (5) dry-run: no Runner invocations, feature not shipped.
func TestLoopDryRunNoSideEffects(t *testing.T) {
	dir := newProject(t)
	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)

	runner := newFakeRunner(t, dir)
	exec := &okExec{}
	notify := &recNotify{}
	opts := baseOpts(dir, runner, exec, notify)
	opts.DryRun = true
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if runner.workerCalls != 0 || runner.reviewerCalls != 0 {
		t.Fatalf("dry-run invoked runner (worker=%d reviewer=%d)", runner.workerCalls, runner.reviewerCalls)
	}
	if exec.calls != 0 {
		t.Fatal("dry-run invoked gate exec")
	}
	if got := featureStatus(t, dir, "f"); got == "shipped" {
		t.Fatal("dry-run shipped the feature")
	}
}

// crashRunner errors on the worker for its first `crashes` calls (simulating a
// transient agent crash / non-zero exit), then checkpoints normally; the reviewer
// always accepts. Used to exercise the soft-failure handling (review C1/I2).
type crashRunner struct {
	dir     string
	crashes int
	wcalls  int
}

func (r *crashRunner) Run(ctx context.Context, lc LaunchContext) error {
	seatID, briefing := lc.Seat, lc.Briefing
	task := taskIDFromBrief(briefing)
	if isWorker(briefing) {
		r.wcalls++
		if r.wcalls <= r.crashes {
			return fmt.Errorf("simulated agent crash %d", r.wcalls)
		}
		return pact.At(r.dir).As(seatID).Checkpoint(task, "ok")
	}
	return pact.At(r.dir).As(seatID).Accept(task)
}

// (C1) a single transient runner crash must NOT kill the driver: it counts as a
// soft failure, the next iteration retries, and the feature still ships.
func TestLoopRunnerCrashIsSoftFailure(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f1", "feat-f1", spec)

	run := &crashRunner{dir: dir, crashes: 1} // crash once, then succeed
	notify := &recNotify{}
	if err := Run(context.Background(), baseOpts(dir, run, &okExec{}, notify)); err != nil {
		t.Fatalf("driver returned error on a transient crash (should be soft): %v", err)
	}
	if got := featureStatus(t, dir, "f1"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped (crash survived + retried)", got)
	}
}

// (C1) a worker that always crashes must escalate at MaxFails, NOT return a
// driver-killing error.
func TestLoopRunnerCrashEscalatesAtFailLimit(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f1", "feat-f1", spec)

	run := &crashRunner{dir: dir, crashes: 99} // never succeeds
	notify := &recNotify{}
	// failExec → the recovery classifier's verify also fails (work genuinely
	// incomplete), so soft-fails accumulate to the limit and escalate. (With a
	// passing verify the driver would auto-checkpoint and recover instead.)
	if err := Run(context.Background(), baseOpts(dir, run, &failExec{}, notify)); err != nil {
		t.Fatalf("always-crash should escalate (paused), not error: %v", err)
	}
	if got := featureStatus(t, dir, "f1"); got == "shipped" {
		t.Fatalf("feature shipped despite a worker that never checkpoints")
	}
	paused := false
	for _, m := range notify.msgs {
		if strings.Contains(m, "paused") {
			paused = true
		}
	}
	if !paused {
		t.Fatalf("expected an escalation/paused notification, got %v", notify.msgs)
	}
}

// (I3) escalate must not panic when Now is nil — it falls back to wall-clock.
func TestEscalateNilNowDoesNotPanic(t *testing.T) {
	dir := newProject(t)
	opts := Options{Dir: dir, Notify: &recNotify{}} // Now intentionally nil
	if err := opts.escalate("t1", "stuck", "evidence", "do X then resume"); err != nil {
		t.Fatalf("escalate with nil Now: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".pact", "orchestrate"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("escalation file not written: err=%v entries=%d", err, len(entries))
	}
}
