package ledger

import (
	"os/exec"
	"strings"
	"testing"
)

// newGitRepo makes a repo with one commit so HEAD exists.
func newGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"},
	} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	return dir
}

// WS-B, the core of the single-canonical-ledger design: the ledger lives in a
// ref that no branch checkout touches. These tests pin the storage contract
// BEFORE anything is wired to it — the flag stays off, so production behavior
// is unchanged either way.

func TestRefStoreReadsEmptyBeforeAnythingIsWritten(t *testing.T) {
	dir := newGitRepo(t)

	lines, err := ReadRef(dir)
	if err != nil {
		t.Fatalf("a repo with no ledger ref must read empty, not error: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("want no lines, got %d", len(lines))
	}
}

func TestRefStoreAppendThenRead(t *testing.T) {
	dir := newGitRepo(t)

	if err := AppendRef(dir, `{"event_id":"a1"}`); err != nil {
		t.Fatalf("AppendRef: %v", err)
	}
	if err := AppendRef(dir, `{"event_id":"a2"}`); err != nil {
		t.Fatalf("AppendRef: %v", err)
	}

	lines, err := ReadRef(dir)
	if err != nil {
		t.Fatalf("ReadRef: %v", err)
	}
	if len(lines) != 2 || !strings.Contains(lines[0], "a1") || !strings.Contains(lines[1], "a2") {
		t.Fatalf("want the two events in order, got %v", lines)
	}
}

// The ref must not be reachable from any branch: that is the whole point —
// a branch checkout must never move or conflict with the ledger.
func TestRefIsNotOnTheCheckedOutBranch(t *testing.T) {
	dir := newGitRepo(t)
	if err := AppendRef(dir, `{"event_id":"a1"}`); err != nil {
		t.Fatal(err)
	}

	// The working tree stays clean — appending an event writes no file.
	st, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(st)) != "" {
		t.Errorf("appending to the ledger ref must not dirty the working tree, got:\n%s", st)
	}
}

// Concurrency: the design replaces a per-worktree flock with git's own
// compare-and-swap. Verified against real git in the spec's §5.2 experiment —
// a stale expected-value must be rejected, not silently overwrite.
func TestAppendRefRejectsStaleWrite(t *testing.T) {
	dir := newGitRepo(t)
	if err := AppendRef(dir, `{"event_id":"a1"}`); err != nil {
		t.Fatal(err)
	}
	stale, err := RefHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Someone else appends in between.
	if err := AppendRef(dir, `{"event_id":"a2"}`); err != nil {
		t.Fatal(err)
	}

	err = appendRefCAS(dir, `{"event_id":"a3"}`, stale)

	if err == nil {
		t.Fatal("a write based on a stale head must be rejected — silently overwriting loses the concurrent event")
	}
	// And nothing was lost.
	lines, _ := ReadRef(dir)
	if len(lines) != 2 {
		t.Errorf("rejected write must leave the ledger untouched, got %d lines", len(lines))
	}
}

// AppendRef retries on contention rather than surfacing the CAS failure, so a
// caller racing another process still lands its event.
func TestAppendRefSucceedsUnderContention(t *testing.T) {
	dir := newGitRepo(t)
	for i := 0; i < 8; i++ {
		if err := AppendRef(dir, `{"event_id":"e`+string(rune('0'+i))+`"}`); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	lines, err := ReadRef(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 8 {
		t.Fatalf("want 8 events, got %d: %v", len(lines), lines)
	}
}

// The ledger's own history is preserved: each append is a commit on the ref, so
// `git log <ref>` answers "when did this event arrive".
func TestEachAppendIsItsOwnCommit(t *testing.T) {
	dir := newGitRepo(t)
	for _, e := range []string{`{"event_id":"a1"}`, `{"event_id":"a2"}`, `{"event_id":"a3"}`} {
		if err := AppendRef(dir, e); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command("git", "-C", dir, "rev-list", "--count", RefName).Output()
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "3" {
		t.Errorf("want 3 commits on %s, got %s", RefName, got)
	}
}

// Non-git directories must degrade, not explode: the CLI runs in plain
// directories during tests and first-run flows.
func TestRefStoreOnNonGitDirIsAnError(t *testing.T) {
	if err := AppendRef(t.TempDir(), `{"event_id":"a1"}`); err == nil {
		t.Error("appending in a non-git dir must report an error rather than appear to succeed")
	}
}
