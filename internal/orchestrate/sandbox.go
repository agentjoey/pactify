package orchestrate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/lockx"
	"github.com/agentjoey/pactify/internal/projection"
)

// sandboxDirName is where the isolated orchestration worktree lives (under the
// already-ignored .pact/orchestrate/ runtime tree).
const sandboxDirName = "sandbox"

// parkBranch is the scratch branch the user's main tree is parked on for the
// duration of a sandbox run, freeing base for the sandbox worktree's merges.
const parkBranch = "pact-sandbox-park"

// logCopybackLockTimeout bounds how long a ledger 回灌 waits for the cross-process
// log lock. A var so tests can shrink it. On expiry the write is SKIPPED — the
// copy-back is best-effort observation, so a busy lock must never stall the loop.
var logCopybackLockTimeout = 30 * time.Second

// RunSandbox drives the serial loop in an ISOLATED git worktree so agent workers
// never checkout/write in the user's active working tree (the build/deploy tree).
//
// Mechanism: the main tree is parked off the base branch for the run so the
// sandbox worktree can hold base for its merges, then restored (carrying any
// merges if it was on base). Feature branches + merges live in the shared .git, so
// the integrated result is visible repo-wide. The pact ledger is gitignored, so it
// is copied INTO the sandbox and the updated ledger copied back out (回灌) — the
// .pact stays local, respecting a repo that gitignores it.
func RunSandbox(ctx context.Context, opts Options) (err error) {
	dir := opts.Dir
	base := gitx.DefaultBranch(dir)
	if base == "" {
		base, _ = gitx.CurrentBranch(dir)
	}
	if base == "" {
		return fmt.Errorf("sandbox: cannot determine base branch (set origin/HEAD or check out a branch)")
	}
	// Crash guard: a surviving park marker (or a tree still sitting on the park
	// branch) means a previous sandbox run died before restoring the user's branch.
	// Re-parking would capture the park branch itself as "orig" and permanently lose
	// the real branch; auto-restoring is unsafe too (the post-crash tree state is
	// unknown). Refuse, name the recorded branch, and let a human look.
	if err := checkStalePark(dir, parkBranch); err != nil {
		return err
	}
	if dirty, _ := gitx.HasChanges(dir); dirty {
		return fmt.Errorf("sandbox: working tree is dirty — commit/stash before an isolated run, or pass --in-place to run directly in your tree (parking needs a clean tree)")
	}

	orig, _ := gitx.CurrentBranch(dir)
	sbDir := filepath.Join(dir, ".pact", "orchestrate", sandboxDirName)

	// Persist the parked branch's identity BEFORE parking: the in-memory orig dies
	// with the process, and after a mid-run crash CurrentBranch reads the park
	// branch itself — the marker is the only record of where the user really was.
	if err := writeParkMarker(dir, orig); err != nil {
		return fmt.Errorf("sandbox: record park marker: %w", err)
	}
	// Park at the CURRENT head, always. A stale park branch from an earlier run
	// points at that run's pre-park commit; reusing it (CheckoutOrCreate) rewound
	// the main tree — including a git-TRACKED .pact — so syncPact seeded the new
	// sandbox from an old ledger and the run died with "feature not found in
	// sandbox" (2026-07-19 e2e F1, deterministic). checkout -B resets the scratch
	// branch to HEAD; any leftover pointer is garbage by definition (the park
	// branch never carries its own commits — teardown restores and deletes it,
	// and a crashed run is caught by checkStalePark above before reaching here).
	if err := gitx.CheckoutReset(dir, parkBranch); err != nil {
		removeParkMarker(dir) // never parked — the marker must not strand a healthy tree
		return fmt.Errorf("sandbox: park main tree: %w", err)
	}
	// teardown removes the worktree and restores the user's branch. It runs BEFORE
	// the ledger is copied back, so the restore checkout sees a clean tree even when
	// .pact is tracked (the 回灌 would otherwise dirty it and block the checkout).
	teardown := func() {
		_ = gitx.RemoveWorktree(dir, sbDir)
		_ = os.RemoveAll(sbDir)
		if orig != "" && gitx.Checkout(dir, orig) == nil {
			removeParkMarker(dir) // only a CONFIRMED restore clears the crash guard
			// Park hygiene (e2e F9 same family): after a confirmed restore the
			// park branch is a stale pointer that can only mislead — delete it.
			// Best-effort: a failed delete leaves exactly the pre-fix state, and
			// the checkout -B above no longer trusts a leftover anyway.
			_ = gitx.DeleteBranch(dir, parkBranch)
		}
	}

	_ = gitx.RemoveWorktree(dir, sbDir)
	_ = os.RemoveAll(sbDir)
	if err := gitx.AddWorktree(dir, sbDir, base, parkBranch); err != nil {
		teardown()
		return fmt.Errorf("sandbox: create worktree on %q: %w", base, err)
	}
	if err := syncPact(dir, sbDir, true); err != nil {
		teardown()
		return fmt.Errorf("sandbox: seed pact state: %w", err)
	}

	o := opts
	o.Dir = sbDir
	// Dashboard-observable runtime (status.json, streams, escalation) goes to the
	// MAIN dir, not the throwaway worktree — else serve, watching <main>/.pact/
	// orchestrate/, sees nothing and the worktree's copy vanishes at teardown
	// (spec coordination-authority P0b). Git work still happens in sbDir.
	o.RuntimeDir = dir
	o.LedgerDir = dir
	// mirrorLedger's "is .pact ignored in the runtime dir" probe cannot change
	// mid-run; wiring the memo here means steady-state loop iterations don't spawn
	// `git check-ignore` (see Options.pactIgnored).
	o.pactIgnoredMemo = &ignoredMemo{}
	// The epilogue runs in a defer so a panic inside the driver loop cannot skip
	// it — that would orphan the worktree, strand the main tree on the park branch,
	// and lose the advanced ledger (the panic still propagates). Ordering is
	// load-bearing: snapshot the sandbox ledger BEFORE the worktree is removed,
	// restore the branch (still-clean tree), then 回灌 onto the restored tree. The
	// setup-failure paths above return before this defer is installed, so the
	// epilogue can never run twice.
	defer func() {
		ledger := readLedger(sbDir)
		teardown()
		werr := writeLedger(dir, ledger)
		if werr == nil {
			return
		}
		// The authoritative回灌 failed. For a repo that git-tracks .pact the mid-run
		// mirror is a no-op, so this is the ONLY write-back — silently dropping it
		// would lose the whole run's checkpoint/accept/merge events while git already
		// merged. Preserve the snapshot for reconciliation, surface loudly, and fail
		// the run (unless it already failed) so the loss can never pass unnoticed (M15).
		path, perr := preserveUnmergedLedger(dir, ledger)
		msg := fmt.Sprintf("sandbox: FAILED to write this run's ledger back to %s (%v)", filepath.Join(dir, ".pact", "log.jsonl"), werr)
		if perr == nil && path != "" {
			msg += fmt.Sprintf(" — its events were saved to %s; reconcile them into the log (git already applied this run's merges)", path)
		} else {
			msg += fmt.Sprintf(" — AND the recovery snapshot could not be saved (%v): this run's events are LOST", perr)
		}
		if o.Notify != nil {
			o.Notify.Notify(msg)
		}
		fmt.Fprintln(os.Stderr, msg) // stderr too, so a headless run without a notifier still records it
		if err == nil {
			err = errors.New(msg)
		}
	}()
	return o.withDefaults().run(ctx)
}

// --- park marker (crash-safe park identity) ------------------------------------

// parkMarker records on disk which branch the main tree was on before RunSandbox
// parked it, so a crash mid-run (SIGKILL/reboot) cannot erase the user's branch.
type parkMarker struct {
	OrigBranch string `json:"orig_branch"`
	TS         string `json:"ts"`
}

// parkMarkerPath is where the park identity lives (under the runtime tree, next
// to the sandbox worktree it guards).
func parkMarkerPath(dir string) string {
	return filepath.Join(dir, ".pact", "orchestrate", "park.json")
}

// writeParkMarker persists the parked tree's original branch before the park
// checkout happens.
func writeParkMarker(dir, orig string) error {
	b, err := json.Marshal(parkMarker{OrigBranch: orig, TS: time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return err
	}
	path := parkMarkerPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// removeParkMarker clears the crash guard; only called once the tree is known
// restored (or was never parked).
func removeParkMarker(dir string) { _ = os.Remove(parkMarkerPath(dir)) }

// checkStalePark refuses to run over the debris of a crashed run (sandbox or
// parallel — both share the park marker, so a crash in either blocks the next run
// of either until a human recovers). It never removes the marker or restores the
// branch itself: the tree state after a crash is unknown, so recovery is a
// deliberate human act, and the error spells out exactly which commands perform
// it. `park` is the caller's park branch (they differ per mode).
func checkStalePark(dir, park string) error {
	cur, _ := gitx.CurrentBranch(dir)
	b, rerr := os.ReadFile(parkMarkerPath(dir))
	if rerr != nil && cur != park {
		return nil // no marker, not parked: a clean tree
	}
	orig := "<your-branch>" // marker missing/unreadable: find it via `git reflog`
	if rerr == nil {
		var m parkMarker
		if json.Unmarshal(b, &m) == nil && m.OrigBranch != "" {
			orig = m.OrigBranch
		}
	}
	return fmt.Errorf("sandbox: a previous sandbox run did not restore this tree (currently on %q, recorded original branch %q) — inspect the tree, then recover with: git -C %s checkout %s && rm -f %s",
		cur, orig, dir, orig, parkMarkerPath(dir))
}

// readLedger snapshots the pact ledger (log + projection) from dir/.pact.
func readLedger(dir string) map[string][]byte {
	m := map[string][]byte{}
	for _, f := range []string{"log.jsonl", "STATE.yml"} {
		if b, err := os.ReadFile(filepath.Join(dir, ".pact", f)); err == nil {
			m[f] = b
		}
	}
	return m
}

// writeLedger MERGES a sandbox ledger snapshot back into dir/.pact (回灌). The log
// is an append-only event stream, so it is unioned by event_id (not overwritten)
// — any event appended to main between seed and copy-back survives — and STATE.yml
// is reprojected from the merged log (authoritative) rather than copied from the
// sandbox's stale projection.
// writeLedger returns an error when the merged log could NOT be written (lock
// contention or a write failure), so the caller can decide whether that loss is
// tolerable: the mid-run mirror ignores it (best-effort — the next iteration
// retries), but the epilogue回灌 is the authoritative, last-chance write-back and
// must NOT drop the run's events silently — it preserves them instead (M15).
func writeLedger(dir string, m map[string][]byte) error {
	sandboxLog := m["log.jsonl"]
	if sandboxLog == nil {
		return nil
	}
	// The read-merge-write below must be atomic against every other pactify log
	// writer (CLI verbs, serve) — an event they append between our read and our
	// rewrite would otherwise be silently dropped (TOCTOU). Serialize on the shared
	// cross-process log lock: GitPath resolves this dir's OWN gitdir (per-worktree
	// in a linked worktree), which matches the ledger's per-worktree scope — every
	// writer of dir's ledger agrees on one lock file. Lock ORDER when both apply:
	// the base-integration lock (pactify-base.lock, held by pact.Merge) is acquired
	// FIRST, then this log lock — never take the base lock while holding the log
	// lock, or two processes deadlock. An unresolvable lock path (dir not a repo)
	// proceeds unlocked; a timeout returns an error (the caller handles the loss).
	if lockPath, lerr := gitx.GitPath(dir, "pactify-log.lock"); lerr == nil {
		lctx, cancel := context.WithTimeout(context.Background(), logCopybackLockTimeout)
		defer cancel()
		release, aerr := lockx.Acquire(lctx, lockPath)
		if aerr != nil {
			return fmt.Errorf("acquire log lock for 回灌: %w", aerr)
		}
		defer release()
	}
	logPath := filepath.Join(dir, ".pact", "log.jsonl")
	mainLog, _ := os.ReadFile(logPath)
	// Temp-file-in-same-dir + rename (atomic on one filesystem) so a concurrent
	// event.ReadAll never observes the log mid-truncate — the same pattern
	// projection.WriteState already uses for STATE.yml.
	if err := atomicWriteFile(logPath, mergeEventLines(mainLog, sandboxLog)); err != nil {
		return fmt.Errorf("write merged log: %w", err)
	}
	if evs, err := event.ReadAll(logPath); err == nil {
		_ = projection.WriteState(filepath.Join(dir, ".pact", "STATE.yml"), projection.Project(evs))
	}
	return nil
}

// preserveUnmergedLedger drops a sandbox ledger snapshot that could not be merged
// back into dir's log (the epilogue回灌 failed) to a timestamped recovery file, so
// the run's events — checkpoints/accepts/merges that git already applied — survive
// for reconciliation instead of vanishing silently (review finding M15). Returns
// the recovery path.
func preserveUnmergedLedger(dir string, m map[string][]byte) (string, error) {
	log := m["log.jsonl"]
	if log == nil {
		return "", nil
	}
	name := fmt.Sprintf("unmerged-ledger-%s.jsonl", time.Now().UTC().Format("20060102-150405.000"))
	path := filepath.Join(dir, ".pact", "orchestrate", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, log, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// atomicWriteFile writes b to path via a temp file in path's own directory plus
// os.Rename, so readers only ever see the old or the new content, never a torn
// intermediate. Same-dir keeps the rename on one filesystem (where it is atomic).
func atomicWriteFile(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// CreateTemp opens 0600; the ledger is world-readable like every pact file.
	if err := os.Chmod(tmp, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// mergeEventLines unions two append-only JSONL event logs by event_id, preserving
// order: every line of base first, then lines of extra whose event_id is new.
// Lines with no event_id are kept as-is (never deduped).
func mergeEventLines(base, extra []byte) []byte {
	seen := map[string]bool{}
	var out [][]byte
	add := func(b []byte) {
		for _, line := range bytes.Split(b, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			if id := lineEventID(line); id != "" {
				if seen[id] {
					continue
				}
				seen[id] = true
			}
			out = append(out, line)
		}
	}
	add(base)
	add(extra)
	if len(out) == 0 {
		return nil
	}
	return append(bytes.Join(out, []byte("\n")), '\n')
}

// lineEventID extracts the event_id from a JSONL event line ("" if absent/unparsable).
func lineEventID(line []byte) string {
	var e struct {
		EventID string `json:"event_id"`
	}
	_ = json.Unmarshal(line, &e)
	return e.EventID
}

// syncPact copies the pact ledger from src/.pact into dst/.pact. On seed it also
// copies the specs/tasks the driver reads; the orchestrate/ runtime is never copied.
func syncPact(src, dst string, seed bool) error {
	files := []string{"log.jsonl", "STATE.yml"}
	if seed {
		files = append(files, "PROJECT.md")
	}
	for _, f := range files {
		if err := copyFile(filepath.Join(src, ".pact", f), filepath.Join(dst, ".pact", f)); err != nil {
			return err
		}
	}
	if seed {
		for _, d := range []string{"specs", "tasks"} {
			_ = copyTree(filepath.Join(src, ".pact", d), filepath.Join(dst, ".pact", d))
		}
	}
	return nil
}

// copyFile copies src→dst (creating parents); a missing src is a no-op.
func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

// copyTree recursively copies the files under src into dst (a missing src is a no-op).
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		return copyFile(p, filepath.Join(dst, rel))
	})
}
