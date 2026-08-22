//go:build agye2e

package orchestrate

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/projection"
)

// agy-e2e (stage 3 of the antigravity integration, .pact/tasks/agy-e2e.md): a
// re-runnable end-to-end regression against the REAL `agy` binary — "real .pact
// project → assign → agy does the work → checkpoint".
//
//	go test -tags agye2e -run 'Antigravity' ./internal/orchestrate/
//
// WHAT THIS DOES AND DOES NOT PROVE. The task premised this file on guarding a
// silent failure mode: agy writing into ~/.gemini/antigravity-cli/scratch/
// instead of RepoDir when --add-dir is missing. The spec required verifying that
// premise by removing the flag and confirming assertion #3 fails. That reverse
// verification was run on 2026-08-22 and the premise DID NOT HOLD: with
// `--add-dir "{repoDir}"` deleted from the antigravity launch profile, this test
// PASSED twice, with zero new scratch entries. The reason is that CmdRunner sets
// cmd.Dir = lc.RepoDir, so agy already has the repo as its cwd; the original
// dogfood that observed the scratch fallback had launched agy from a different
// directory. Assertion #3 is therefore a guard that cannot currently fire, kept
// as a canary in case the cwd contract changes — NOT evidence that --add-dir is
// load-bearing. See the launch profile comment in internal/agent/launch.go.
//
// The real value this file delivers is elsewhere, and was found by running it:
// it caught that agy exits 0 while reporting {"status":"ERROR"} after a rejected
// tool call, which no unit test would have surfaced. See parseAntigravityStatus.
//
// Gated behind `//go:build agye2e` (the acp_smoke_test.go `acpsmoke` idiom). The
// original version was deliberately untagged so the task's own verify line would
// pick it up, but the cost of that is borne by everyone: on any machine with agy
// installed and authenticated, a plain `go test ./internal/orchestrate/` spawns
// two real LLM stints (8-minute timeouts), builds a pactify binary, and reads the
// user's real ~/.gemini scratch directory — and it is non-deterministic, because
// whether agy reaches for a shell command or its native write tool is a model
// decision that varies run to run. The runtime requireAgy skip is kept as a
// second gate for anyone who does build with the tag but has no agy.
//
// requireAgy is the skip gate: exec.LookPath("agy") plus the OAuth token
// file's existence, mirroring acp_smoke_test.go's "skip cleanly when the
// vendor binary isn't on PATH" idiom but extended with the auth check this
// spec calls for (a `agy` on PATH with no token would otherwise hang on an
// interactive login prompt instead of failing fast).
func requireAgy(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("agy"); err != nil {
		t.Skip("agy not on PATH — skipping real antigravity e2e (agy-e2e stage 3)")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot resolve $HOME — skipping real antigravity e2e")
	}
	tok := filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	if _, err := os.Stat(tok); err != nil {
		t.Skip("agy not authenticated (no ~/.gemini/antigravity-cli/antigravity-oauth-token) — skipping real antigravity e2e")
	}
}

// agyE2ERoot creates a throwaway root for one test's repo + built pactify
// binary, cleaned up on test exit.
//
// It is rooted under $HOME rather than t.TempDir(), a deliberate divergence from
// the rest of this package's test idiom.
//
// The ORIGINAL justification for that divergence was wrong and has been removed:
// it claimed agy's --add-dir validation REJECTS paths under macOS's per-process
// $TMPDIR (/var/folders/.../T/) and accepts them under $HOME. Re-tested 2026-08-22
// as a controlled A/B — same prompt, same flags, agy writing via its native tool
// into a $TMPDIR repo and a $HOME repo, twice each — all four SUCCEEDED. Location
// is not the variable.
//
// What actually varies is whether the model emits an ABSOLUTE path in its
// write_to_file call, which agy's permission layer rejects
// ("<path> is not a valid artifact path; artifacts must be in
// .../antigravity-cli/brain/<id>/") regardless of where the workspace lives. That
// is a per-run model decision, which is why this file is now build-tagged: the
// rejection is intermittent and agy usually recovers via a shell command.
//
// $HOME rooting is KEPT anyway, for a reason that does survive scrutiny: agy
// resolves and displays workspace paths, and macOS $TMPDIR entries are
// symlinked (/var → /private/var), so a repo there gives the agent two spellings
// of its own workspace. Keeping the tree at a stable real path removes that
// variable from an already non-deterministic test. The tree is removed on exit.
func agyE2ERoot(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve $HOME: %v", err)
	}
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	root := filepath.Join(home, ".pactify-agy-e2e-tmp", name+"-"+time.Now().Format("20060102-150405.000000000"))
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir agy e2e root: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return root
}

// buildPactifyBin builds the pactify binary from THIS checkout's source (not
// whatever `pactify` happens to be on the machine's real PATH) into root/bin,
// so the agy worker/reviewer stints run the exact protocol semantics this
// branch defines. Returns the binary's directory (for prepending to PATH).
func buildPactifyBin(t *testing.T, root string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve module root: runtime.Caller failed")
	}
	modRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	out := filepath.Join(binDir, "pactify")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/pactify")
	cmd.Dir = modRoot
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build pactify for agy e2e: %v\n%s", err, b)
	}
	return binDir
}

// prependPath puts dir first on PATH for the remainder of the test (so the
// `pactify` agy's shell tool calls resolves is the freshly-built one).
func prependPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// commitInitScaffold commits pact.Project.Init's on-disk side effects (the
// baked seat entry files, e.g. GEMINI.md/CLAUDE.md — Init writes them but does
// not commit them) onto whatever branch is currently checked out. Without
// this they stay untracked, and the FIRST real commit made on a later feature
// branch (agy's own `pactify checkpoint` → CommitAll's `git add -A`) sweeps
// them in alongside the agent's actual work — polluting the "no out-of-scope
// files" diff with roster scaffolding that predates the task entirely
// (verified 2026-08-22: CLAUDE.md/GEMINI.md/R.md showed up next to
// agy_marker.txt in the feature-branch diff before this fix). Committing them
// to base immediately after Init — mirroring how a real pact project commits
// its onboarding output — keeps them identical on base and every feature
// branch, so they never appear in a branch diff again.
func commitInitScaffold(t *testing.T, repo string) {
	t.Helper()
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "pact init")
}

// gitRun runs a git subcommand in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newAgyGitRepo scaffolds a fresh git repo (under root, itself $HOME-rooted —
// see agyE2ERoot) with an initial commit and a .gitignore that excludes
// `.pact/` from every commit, so the "no out-of-scope files" assertions below
// see cleanly only the files agy itself created/edited.
func newAgyGitRepo(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "agy-e2e@test.local")
	gitRun(t, dir, "config", "user.name", "agy-e2e")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".pact/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("agy e2e scratch repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	return dir
}

// antigravityScratchDir is the directory the silent-failure bug writes into
// when --add-dir is missing (see launch.go's antigravity RunnerProfile
// comment). Overridable indirection kept minimal — always the real path,
// since the guardrail is only meaningful against the real agy state dir.
func antigravityScratchDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve $HOME: %v", err)
	}
	return filepath.Join(home, ".gemini", "antigravity-cli", "scratch")
}

// snapshotDirEntries returns the set of relative paths under dir (files and
// subdirectories alike). A missing dir yields an empty set — tolerated, since
// a fresh machine may not have created ~/.gemini/antigravity-cli/scratch/ yet.
func snapshotDirEntries(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries := map[string]bool{}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return nil //nolint:nilerr // best-effort snapshot; a walk error just yields a smaller (but honest) set
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr == nil {
			entries[rel] = true
		}
		return nil
	})
	return entries
}

// newScratchEntries returns entries present in after but not before.
func newScratchEntries(before, after map[string]bool) []string {
	var out []string
	for k := range after {
		if !before[k] {
			out = append(out, k)
		}
	}
	return out
}

// mustFindTask locates taskID in st or fails the test.
func mustFindTask(t *testing.T, st projection.State, taskID string) projection.Task {
	t.Helper()
	for _, f := range st.Features {
		for _, tk := range f.Tasks {
			if tk.ID == taskID {
				return tk
			}
		}
	}
	t.Fatalf("task %q not found in state:\n%+v", taskID, st)
	return projection.Task{}
}

// mustFindSeat locates seatID in st's roster or fails the test.
func mustFindSeat(t *testing.T, st projection.State, seatID string) projection.Seat {
	t.Helper()
	for _, s := range st.Agents {
		if s.ID == seatID {
			return s
		}
	}
	t.Fatalf("seat %q not found in roster:\n%+v", seatID, st.Agents)
	return projection.Seat{}
}

// readLedgerEvents reads every event from repo's ledger, failing the test on error.
func readLedgerEvents(t *testing.T, repo string) []event.Event {
	t.Helper()
	evs, err := event.ReadAll(paths.LogIn(repo))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	return evs
}

// TestAntigravityWorkerDeliversIntoRepoDir is the dogfood worker-path
// regression (spec §"Core assertions — worker path"): a real `.pact` project,
// a tiny real task assigned to an `antigravity`-kind seat, driven through the
// production RunnerProfileFor("antigravity")/CmdRunner codepath (not a
// hand-built agy invocation), asserting all four required outcomes plus the
// no-self-accept invariant.
func TestAntigravityWorkerDeliversIntoRepoDir(t *testing.T) {
	requireAgy(t)
	// Isolate agentcfg/roles resolution from this machine's real agent
	// registry, so the launch uses RunnerProfileFor("antigravity")'s built-in
	// defaults (model gemini-3.7-flash-medium, no --effort) rather than
	// whatever overrides happen to be configured on the developer's machine.
	t.Setenv("PACTIFY_HOME", t.TempDir())

	root := agyE2ERoot(t)
	binDir := buildPactifyBin(t, root)
	prependPath(t, binDir)
	repo := newAgyGitRepo(t, root)

	base, err := gitx.CurrentBranch(repo)
	if err != nil || base == "" {
		t.Fatalf("resolve base branch: %v (branch=%q)", err, base)
	}

	const taskID = "t-agy-worker"
	const feature = "f-agy-worker"
	const branch = "feat/agy-worker"
	const seatID = "agy-w"
	const specRel = ".pact/tasks/agy-e2e-worker.md"

	specAbs := filepath.Join(repo, specRel)
	if err := os.MkdirAll(filepath.Dir(specAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := "# " + taskID + "\n\n" +
		"Create a file named `agy_marker.txt` at the REPO ROOT (the current\n" +
		"working directory) containing exactly one line: agy-e2e-worker-ok\n\n" +
		"Do not create, edit, or delete any other file.\n\n" +
		"Verify: `test -f agy_marker.txt && grep -qx agy-e2e-worker-ok agy_marker.txt`\n"
	if err := os.WriteFile(specAbs, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	orch := pact.At(repo).As("orch")
	seats := []string{
		"orch:orchestrator:CLAUDE.md",
		seatID + ":worker:GEMINI.md",
		"rev:reviewer:R.md",
	}
	if err := orch.Init("agy-e2e-worker", seats); err != nil {
		t.Fatalf("init: %v", err)
	}
	commitInitScaffold(t, repo)
	if err := orch.Assign(taskID, feature, branch, seatID, "rev", specRel, nil); err != nil {
		t.Fatalf("assign: %v", err)
	}

	// Mirror the real driver (runOwner in loop.go): check out the task's
	// feature branch before launching the worker, so the worker's own
	// `pactify join` (which the briefing instructs it to run) lands on the
	// right branch rather than whichever one join's own heuristic would pick.
	if err := gitx.CheckoutOrCreate(repo, branch); err != nil {
		t.Fatalf("checkout feature branch: %v", err)
	}

	st, err := pact.At(repo).StateProjection()
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	seat := mustFindSeat(t, st, seatID)
	task := mustFindTask(t, st, taskID)
	briefing := workerBrief(repo, seat, task, "", false)

	scratch := antigravityScratchDir(t)
	scratchBefore := snapshotDirEntries(t, scratch)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	runner := NewCmdRunner(0)
	lc := LaunchContext{
		Seat: seatID, Kind: "antigravity", Task: taskID, Project: "agy-e2e-worker",
		Briefing: briefing, RepoDir: repo,
	}
	if err := runner.Run(ctx, lc); err != nil {
		t.Fatalf("agy worker stint failed: %v", err)
	}

	// --- Assertion #3 (dedicated scratch-dir guardrail) ---
	scratchAfter := snapshotDirEntries(t, scratch)
	if newer := newScratchEntries(scratchBefore, scratchAfter); len(newer) > 0 {
		t.Fatalf("assertion #3: agy wrote into ~/.gemini/antigravity-cli/scratch/ instead of RepoDir — new entries: %v", newer)
	}
	markerPath := filepath.Join(repo, "agy_marker.txt")
	markerBytes, rerr := os.ReadFile(markerPath)
	if rerr != nil {
		t.Fatalf("assertion #3: marker file must land inside RepoDir at %s, got: %v", markerPath, rerr)
	}
	if got := strings.TrimSpace(string(markerBytes)); got != "agy-e2e-worker-ok" {
		t.Fatalf("marker file content = %q, want %q", got, "agy-e2e-worker-ok")
	}

	// --- Assertion #1: the seat's checkpoint event appears in the ledger ---
	evs := readLedgerEvents(t, repo)
	checkpointed := false
	selfAccepted := false
	for _, e := range evs {
		if e.TaskID != taskID {
			continue
		}
		switch e.EventType {
		case "checkpoint":
			if e.AgentID != seatID {
				t.Fatalf("checkpoint event agent_id = %q, want %q (identity propagation broken)", e.AgentID, seatID)
			}
			checkpointed = true
		case "accept":
			if e.AgentID == seatID {
				selfAccepted = true
			}
		}
	}
	if !checkpointed {
		t.Fatalf("assertion #1: no checkpoint event for %s by %s found in the ledger:\n%+v", taskID, seatID, evs)
	}
	// --- Invariant: agy as owner must NOT self-accept its own task ---
	if selfAccepted {
		t.Fatalf("invariant violated: agy self-accepted its own task %s (owner==reviewer forbidden)", taskID)
	}

	// --- Assertion #2: task state is awaiting_review, never self-marked accepted ---
	st2, err := pact.At(repo).StateProjection()
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	tk := mustFindTask(t, st2, taskID)
	if tk.Status != "awaiting_review" {
		t.Fatalf("assertion #2: task status = %q, want awaiting_review", tk.Status)
	}

	// --- Assertion #4: zero out-of-scope edits ---
	// (a) the feature branch's diff vs base contains exactly the spec-allowed file.
	diffOut, err := exec.Command("git", "-C", repo, "diff", "--name-only", base, branch).Output()
	if err != nil {
		t.Fatalf("git diff --name-only %s %s: %v", base, branch, err)
	}
	changed := strings.Fields(string(diffOut))
	if len(changed) != 1 || changed[0] != "agy_marker.txt" {
		t.Fatalf("assertion #4: out-of-scope files touched — got %v, want exactly [agy_marker.txt]", changed)
	}
	// (b) the working tree is fully clean — no stray uncommitted/untracked junk
	// outside .gitignore that a name-only branch diff wouldn't catch.
	statusOut, err := exec.Command("git", "-C", repo, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status --porcelain: %v", err)
	}
	if s := strings.TrimSpace(string(statusOut)); s != "" {
		t.Fatalf("assertion #4: working tree not clean after the stint (stray/uncommitted files):\n%s", s)
	}
}

// TestAntigravityReviewerRendersVerdict is the dogfood reviewer-path
// regression: a task already sitting at awaiting_review (checkpointed by a
// non-agy fake worker, so this test isolates the REVIEWER behavior) is
// reviewed by a real `antigravity`-kind seat, asserting it renders a verdict
// (accept or changes_requested — either is an acceptable real judgment) whose
// ledger event carries the reviewer's own seat id.
func TestAntigravityReviewerRendersVerdict(t *testing.T) {
	requireAgy(t)
	t.Setenv("PACTIFY_HOME", t.TempDir())

	root := agyE2ERoot(t)
	binDir := buildPactifyBin(t, root)
	prependPath(t, binDir)
	repo := newAgyGitRepo(t, root)

	const taskID = "t-agy-reviewer"
	const feature = "f-agy-reviewer"
	const branch = "feat/agy-reviewer"
	const workerSeat = "fake-w"
	const reviewerSeat = "agy-r"
	const specRel = ".pact/tasks/agy-e2e-reviewer.md"

	specAbs := filepath.Join(repo, specRel)
	if err := os.MkdirAll(filepath.Dir(specAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := "# " + taskID + "\n\n" +
		"`marker.txt` must exist at the repo root containing exactly one line:\n" +
		"agy-e2e-reviewer-ok\n\n" +
		"Verify: `test -f marker.txt && grep -qx agy-e2e-reviewer-ok marker.txt`\n"
	if err := os.WriteFile(specAbs, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	orch := pact.At(repo).As("orch")
	seats := []string{
		"orch:orchestrator:CLAUDE.md",
		workerSeat + ":worker:W.md",
		reviewerSeat + ":reviewer:GEMINI.md",
	}
	if err := orch.Init("agy-e2e-reviewer", seats); err != nil {
		t.Fatalf("init: %v", err)
	}
	commitInitScaffold(t, repo)
	if err := orch.Assign(taskID, feature, branch, workerSeat, reviewerSeat, specRel, nil); err != nil {
		t.Fatalf("assign: %v", err)
	}

	// Get the task to awaiting_review WITHOUT agy: a plain fake worker checks
	// out the branch, does the (trivial, spec-correct) work, and checkpoints.
	// This isolates the reviewer path — only the verdict below is real agy.
	fw := pact.At(repo).As(workerSeat)
	if err := fw.JoinTask(workerSeat, "worker", taskID); err != nil {
		t.Fatalf("fake worker join: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "marker.txt"), []byte("agy-e2e-reviewer-ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fw.Checkpoint(taskID, "created marker.txt per spec (fake worker, non-agy)"); err != nil {
		t.Fatalf("fake worker checkpoint: %v", err)
	}

	st, err := pact.At(repo).StateProjection()
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	task := mustFindTask(t, st, taskID)
	if task.Status != "awaiting_review" {
		t.Fatalf("precondition: task must be awaiting_review before driving the reviewer, got %q", task.Status)
	}
	// Matches production's launchReviewers (loop.go): the reviewer briefing is
	// built from a bare Seat{ID: ...} — reviewerBrief only reads seat.ID.
	briefing := reviewerBrief(repo, projection.Seat{ID: reviewerSeat}, task, "")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	runner := NewCmdRunner(0)
	lc := LaunchContext{
		Seat: reviewerSeat, Kind: "antigravity", Task: taskID, Project: "agy-e2e-reviewer",
		Briefing: briefing, RepoDir: repo,
	}
	if err := runner.Run(ctx, lc); err != nil {
		t.Fatalf("agy reviewer stint failed: %v", err)
	}

	evs := readLedgerEvents(t, repo)
	var verdict *event.Event
	for i := range evs {
		e := &evs[i]
		if e.TaskID != taskID {
			continue
		}
		if e.EventType == "accept" || e.EventType == "changes_requested" {
			verdict = e
		}
	}
	if verdict == nil {
		t.Fatalf("reviewer path: no accept/changes_requested verdict event for %s found in the ledger:\n%+v", taskID, evs)
	}
	// --- Invariant: the verdict event's agent_id is the reviewer seat ---
	if verdict.AgentID != reviewerSeat {
		t.Fatalf("verdict event agent_id = %q, want reviewer seat %q (event: %+v)", verdict.AgentID, reviewerSeat, *verdict)
	}
	t.Logf("agy reviewer verdict: %s by %s", verdict.EventType, verdict.AgentID)
}
