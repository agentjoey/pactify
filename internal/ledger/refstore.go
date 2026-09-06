package ledger

import (
	"fmt"
	"os/exec"
	"strings"
)

// RefName is where the canonical ledger lives.
//
// It is an ORPHAN BRANCH, not a custom ref namespace like refs/pact/*, and that
// choice is load-bearing (spec §5.2, decided against `refs/pact/ledger` after a
// real-git experiment):
//
//   - a plain `git clone` fetches refs/heads/* and NOTHING else, so a custom ref
//     arrives only after every clone hand-edits remote.origin.fetch;
//   - the working-tree copy is no longer git-tracked (spec §5.1), so the ledger's
//     entire presence in git rests on this ref. If it does not travel with a
//     clone, a fresh clone gets a repo with NO ledger at all — worse than today.
//
// The cost is one extra row in `git branch -a`. It is never checked out, so a
// branch switch cannot move it and a merge cannot conflict with it.
const RefName = "refs/heads/pact-ledger"

// logFileName is the blob's path inside the ref's tree. It mirrors the exported
// working-tree file so `git show pact-ledger:log.jsonl` reads naturally.
const logFileName = "log.jsonl"

// maxCASRetries bounds the read-modify-write loop when another process appends
// between our read and our update-ref. Contention here is two agents recording
// events at the same instant; a handful of retries covers it without spinning.
const maxCASRetries = 8

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimRight(string(out), "\n"), fmt.Errorf("git %s: %w (%s)",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// RefHead returns the commit the ledger ref points at, or "" when the ref does
// not exist yet. A missing ref is the empty ledger, never an error.
func RefHead(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--verify", "--quiet", RefName)
	if err != nil {
		// `--verify --quiet` exits non-zero for a missing ref. Distinguish that
		// from "this isn't a git repo", which callers must not mistake for empty.
		if _, gerr := git(dir, "rev-parse", "--git-dir"); gerr != nil {
			return "", fmt.Errorf("ledger: not a git repository: %w", gerr)
		}
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// ReadRef returns the ledger's event lines from the ref. An absent ref reads as
// an empty ledger — an uninitialized project is not an error for a reader.
func ReadRef(dir string) ([]string, error) {
	head, err := RefHead(dir)
	if err != nil {
		return nil, err
	}
	if head == "" {
		return nil, nil
	}
	blob, err := git(dir, "show", head+":"+logFileName)
	if err != nil {
		return nil, fmt.Errorf("ledger: read %s: %w", RefName, err)
	}
	if strings.TrimSpace(blob) == "" {
		return nil, nil
	}
	return strings.Split(strings.TrimRight(blob, "\n"), "\n"), nil
}

// AppendRef appends one event line, retrying when another writer wins the race.
func AppendRef(dir, line string) error {
	var lastErr error
	for i := 0; i < maxCASRetries; i++ {
		head, err := RefHead(dir)
		if err != nil {
			return err
		}
		if err := appendRefCAS(dir, line, head); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return fmt.Errorf("ledger: append to %s lost %d compare-and-swap races: %w",
		RefName, maxCASRetries, lastErr)
}

// appendRefCAS writes one event assuming the ref is still at expected, and fails
// if it moved.
//
// This is the replacement for the per-worktree flock: `git update-ref <ref>
// <new> <old>` is an atomic compare-and-swap inside git itself, so it holds
// across worktrees and across processes. The old lock could not: it resolved
// through `--git-path`, which in a linked worktree points at
// .git/worktrees/<name>/ — a DIFFERENT lock file per worktree (verified).
func appendRefCAS(dir, line, expected string) error {
	if _, err := git(dir, "rev-parse", "--git-dir"); err != nil {
		return fmt.Errorf("ledger: not a git repository: %w", err)
	}

	var existing []string
	if expected != "" {
		blob, err := git(dir, "show", expected+":"+logFileName)
		if err != nil {
			return fmt.Errorf("ledger: read current ledger: %w", err)
		}
		if s := strings.TrimRight(blob, "\n"); s != "" {
			existing = strings.Split(s, "\n")
		}
	}
	content := strings.Join(append(existing, strings.TrimRight(line, "\n")), "\n") + "\n"

	blobSHA, err := hashObject(dir, content)
	if err != nil {
		return err
	}
	treeSHA, err := mkTree(dir, blobSHA)
	if err != nil {
		return err
	}

	args := []string{"commit-tree", treeSHA, "-m", "pact: append event"}
	if expected != "" {
		args = append(args, "-p", expected)
	}
	commitSHA, err := git(dir, args...)
	if err != nil {
		return fmt.Errorf("ledger: commit-tree: %w", err)
	}
	commitSHA = strings.TrimSpace(commitSHA)

	// The two-argument form is the compare-and-swap: git refuses when the ref no
	// longer equals `expected`. An empty expected means "must not exist yet",
	// spelled as the all-zero object id.
	old := expected
	if old == "" {
		old = "0000000000000000000000000000000000000000"
	}
	if _, err := git(dir, "update-ref", RefName, commitSHA, old); err != nil {
		return fmt.Errorf("ledger: concurrent write to %s: %w", RefName, err)
	}
	return nil
}

func hashObject(dir, content string) (string, error) {
	cmd := exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ledger: hash-object: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func mkTree(dir, blobSHA string) (string, error) {
	cmd := exec.Command("git", "mktree")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(fmt.Sprintf("100644 blob %s\t%s\n", blobSHA, logFileName))
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ledger: mktree: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
