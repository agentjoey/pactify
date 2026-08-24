package orchestrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/gitx"
)

// --- fixtures -----------------------------------------------------------------

// trackPact commits everything newProject scaffolded, so the repo git-TRACKS its
// own ledger (.pact/log.jsonl + .pact/STATE.yml). Uncommon for user projects,
// but a deliberate, supported choice — this repo itself does it for a full audit
// history — and the shape in which sandbox orchestration used to be unusable.
func trackPact(t *testing.T, dir string) {
	t.Helper()
	gitCommitAll(t, dir, "track the pact ledger")
	if !gitx.PathTracked(dir, filepath.Join(dir, ".pact", "log.jsonl")) {
		t.Fatal("fixture broken: .pact/log.jsonl should be tracked")
	}
}

// ledgerEvent is a well-formed (parseable, projectable) ledger line, so the
// reprojection inside writeLedger sees a real event stream.
func ledgerEvent(id string) string {
	return `{"event_id":"` + id + `","ts":"2026-07-23T00:00:00Z","agent_id":"orch","role":"orchestrator","event_type":"config_gate","payload":{"gate":"` + id + `"}}` + "\n"
}

// appendEvent appends an event straight to the ledger file — exactly what
// syncPact's raw copy does to a sandbox worktree: a working-tree modification of
// a (here tracked) path, made behind the pact engine's back.
func appendEvent(t *testing.T, dir, id string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, ".pact", "log.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(ledgerEvent(id)); err != nil {
		t.Fatalf("append event: %v", err)
	}
}

func readLog(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".pact", "log.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	return string(b)
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return string(out)
}

// --- tests --------------------------------------------------------------------

// THE BUG: in a repo that git-tracks .pact, the sandbox seeds the worktree's
// ledger by raw copy (uncommitted changes to tracked paths), then the driver
// checks out the task's feature branch — and git refuses when that branch
// carries a different ledger snapshot ("Your local changes ... would be
// overwritten by checkout"), which is what forced users onto risky --in-place.
// checkoutFeatureBranch must land on the branch AND keep both ledgers' events.
func TestCheckoutFeatureBranch_TrackedLedgerDivergentBranch(t *testing.T) {
	dir := newProject(t)
	trackPact(t, dir)
	base, _ := gitx.CurrentBranch(dir)

	// A feature branch that already carries its OWN ledger snapshot.
	if err := gitx.CheckoutOrCreate(dir, "feat-a"); err != nil {
		t.Fatalf("create feat-a: %v", err)
	}
	appendEvent(t, dir, "branchEV")
	gitT(t, dir, "commit", "-q", "-am", "branch ledger")
	gitT(t, dir, "checkout", "-q", base)

	// The sandbox seed: a dirty, tracked ledger.
	appendEvent(t, dir, "seedEV")

	// Premise check — this is exactly the failure being fixed.
	if err := gitx.CheckoutOrCreate(dir, "feat-a"); err == nil {
		t.Fatal("premise broken: plain CheckoutOrCreate no longer fails on a tracked, dirty ledger")
	}

	if err := checkoutFeatureBranch(dir, "feat-a"); err != nil {
		t.Fatalf("checkoutFeatureBranch: %v", err)
	}
	if b, _ := gitx.CurrentBranch(dir); b != "feat-a" {
		t.Fatalf("not on the feature branch, got %q", b)
	}
	log := readLog(t, dir)
	if !strings.Contains(log, "seedEV") {
		t.Fatal("the seeded ledger was DESTROYED by the checkout (worse than failing)")
	}
	if !strings.Contains(log, "branchEV") {
		t.Fatal("the branch's own ledger events were clobbered — the merge must be a union")
	}
	if dirty, _ := gitx.HasChanges(dir); dirty {
		t.Fatalf("tree left dirty after checkout:\n%s", gitOut(t, dir, "status", "--porcelain"))
	}
}

// The checkout -b path: the feature branch does not exist yet. It must still
// work, and the seeded ledger must ride along onto the new branch.
func TestCheckoutFeatureBranch_TrackedLedgerNewBranch(t *testing.T) {
	dir := newProject(t)
	trackPact(t, dir)
	appendEvent(t, dir, "seedEV")

	if err := checkoutFeatureBranch(dir, "feat-new"); err != nil {
		t.Fatalf("checkoutFeatureBranch: %v", err)
	}
	if b, _ := gitx.CurrentBranch(dir); b != "feat-new" {
		t.Fatalf("not on the new branch, got %q", b)
	}
	if !strings.Contains(readLog(t, dir), "seedEV") {
		t.Fatal("seeded ledger lost on the newly created branch")
	}
	if dirty, _ := gitx.HasChanges(dir); dirty {
		t.Fatalf("tree left dirty:\n%s", gitOut(t, dir, "status", "--porcelain"))
	}
}

// REGRESSION GUARD for the common case: when .pact is NOT tracked (gitignored,
// the shape RunSandbox was designed around) checkoutFeatureBranch must be plain
// CheckoutOrCreate — no restore, no ledger rewrite, and above all NO extra
// commit. Proved by running both against identical fixtures and comparing the
// observable results.
func TestCheckoutFeatureBranch_UntrackedLedgerIsPlainCheckout(t *testing.T) {
	fixture := func() string {
		dir := newProject(t)
		if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".pact/\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCommitAll(t, dir, "ignore .pact")
		appendEvent(t, dir, "seedEV")
		return dir
	}
	plain, wrapped := fixture(), fixture()
	// Each fixture's ledger carries its own random init event_id, so the ledgers
	// are compared to THEMSELVES before/after, not to each other.
	plainBefore, wrappedBefore := readLog(t, plain), readLog(t, wrapped)

	if err := gitx.CheckoutOrCreate(plain, "feat-x"); err != nil {
		t.Fatalf("CheckoutOrCreate: %v", err)
	}
	if err := checkoutFeatureBranch(wrapped, "feat-x"); err != nil {
		t.Fatalf("checkoutFeatureBranch: %v", err)
	}

	for _, probe := range [][]string{
		{"log", "--format=%s"},          // commit history: no "pact: ledger sync"
		{"status", "--porcelain"},       // working-tree state
		{"branch", "--show-current"},    // where we landed
		{"rev-list", "--count", "HEAD"}, // commit count
	} {
		want, got := gitOut(t, plain, probe...), gitOut(t, wrapped, probe...)
		if want != got {
			t.Fatalf("git %v differs from a plain checkout\nplain:   %q\nwrapped: %q", probe, want, got)
		}
	}
	if got := readLog(t, wrapped); got != wrappedBefore {
		t.Fatalf("an untracked ledger must be left byte-identical\nbefore: %q\nafter:  %q", wrappedBefore, got)
	}
	if got := readLog(t, plain); got != plainBefore {
		t.Fatalf("control: plain CheckoutOrCreate changed the ledger\nbefore: %q\nafter:  %q", plainBefore, got)
	}
	if strings.Contains(gitOut(t, wrapped, "log", "--format=%s"), "ledger sync") {
		t.Fatal("an untracked .pact must never produce a ledger commit")
	}
}

// LOAD-BEARING ABORT: the restore step is destructive, so a snapshot that could
// not be read must stop the whole operation BEFORE anything is discarded —
// never "restore first, discover the loss later".
func TestCheckoutFeatureBranch_AbortsWhenSnapshotUnreadable(t *testing.T) {
	dir := newProject(t)
	trackPact(t, dir)
	base, _ := gitx.CurrentBranch(dir)
	if err := gitx.CheckoutOrCreate(dir, "feat-a"); err != nil {
		t.Fatalf("create feat-a: %v", err)
	}
	appendEvent(t, dir, "branchEV")
	gitT(t, dir, "commit", "-q", "-am", "branch ledger")
	gitT(t, dir, "checkout", "-q", base)

	// Ledger gone from the working tree but still in the index: readLedger
	// returns no log, and a restore would RESURRECT the file — masking the
	// anomaly and then carrying an empty snapshot across the checkout.
	logPath := filepath.Join(dir, ".pact", "log.jsonl")
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}

	if err := checkoutFeatureBranch(dir, "feat-a"); err == nil {
		t.Fatal("an unreadable ledger snapshot must abort the checkout")
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatal("aborting must discard NOTHING — the working tree was modified anyway")
	}
	if b, _ := gitx.CurrentBranch(dir); b != base {
		t.Fatalf("aborting must not switch branches, landed on %q", b)
	}
}

// If the checkout itself fails after the destructive restore, the snapshot must
// be put back — a failed checkout must leave the seeded ledger exactly as it was,
// not silently swallowed by the restore.
func TestCheckoutFeatureBranch_RestoresSeedWhenCheckoutFails(t *testing.T) {
	dir := newProject(t)
	trackPact(t, dir)
	// feat-a is held by ANOTHER worktree, so checking it out here can never work.
	gitT(t, dir, "branch", "feat-a")
	gitT(t, dir, "worktree", "add", t.TempDir(), "feat-a")
	appendEvent(t, dir, "seedEV")

	if err := checkoutFeatureBranch(dir, "feat-a"); err == nil {
		t.Fatal("checkout of a branch held by another worktree should fail")
	}
	if !strings.Contains(readLog(t, dir), "seedEV") {
		t.Fatal("a failed checkout swallowed the seeded ledger (the restore was not rolled back)")
	}
}

// END-TO-END: the whole point of the fix. A repo that git-tracks its ledger,
// with the task's feature branch ALREADY existing and carrying a divergent
// ledger snapshot, must be drivable by the safe DEFAULT sandbox mode — this is
// the run that used to die on "Your local changes ... would be overwritten by
// checkout" and pushed users onto --in-place.
func TestRunSandbox_TrackedLedgerExistingFeatureBranch(t *testing.T) {
	dir := newProject(t)
	trackPact(t, dir)
	base, _ := gitx.CurrentBranch(dir)

	// feat-a already exists from an earlier stint, branched off base and carrying
	// its own ledger snapshot.
	if err := gitx.CheckoutOrCreate(dir, "feat-a"); err != nil {
		t.Fatalf("create feat-a: %v", err)
	}
	appendEvent(t, dir, "branchEV")
	gitT(t, dir, "commit", "-q", "-am", "earlier stint ledger")
	gitT(t, dir, "checkout", "-q", base)

	// The user drives from a working branch whose ledger has moved AHEAD of base
	// (the plan/assignment was recorded there). This is what makes syncPact's raw
	// seed a real modification of tracked paths inside the sandbox worktree, which
	// git then refuses to check out feat-a over.
	gitT(t, dir, "checkout", "-q", "-b", "work")
	writeSpec(t, dir, "ta", "true")
	assignNoCheckout(t, dir, "ta", "fa", "feat-a", filepath.Join(".pact", "tasks", "ta.md"))
	gitCommitAll(t, dir, "plan ta on the working branch")

	before := revParse(t, dir, base)
	if err := RunSandbox(context.Background(), sandboxOpts(t, dir)); err != nil {
		t.Fatalf("RunSandbox on a tracked-.pact repo: %v", err)
	}
	if revParse(t, dir, base) == before {
		t.Fatal("base did not advance — the tracked-ledger sandbox run did not ship")
	}
}
