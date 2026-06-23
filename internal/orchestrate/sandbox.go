package orchestrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/gitx"
)

// sandboxDirName is where the isolated orchestration worktree lives (under the
// already-ignored .pact/orchestrate/ runtime tree).
const sandboxDirName = "sandbox"

// RunSandbox drives the serial loop in an ISOLATED git worktree so agent workers
// never checkout/write in the user's active working tree (the build/deploy tree).
//
// Mechanism: the main tree is parked off the base branch for the run so the
// sandbox worktree can hold base for its merges, then restored (carrying any
// merges if it was on base). Feature branches + merges live in the shared .git, so
// the integrated result is visible repo-wide. The pact ledger is gitignored, so it
// is copied INTO the sandbox and the updated ledger copied back out (回灌) — the
// .pact stays local, respecting a repo that gitignores it.
func RunSandbox(ctx context.Context, opts Options) error {
	dir := opts.Dir
	base := gitx.DefaultBranch(dir)
	if base == "" {
		base, _ = gitx.CurrentBranch(dir)
	}
	if base == "" {
		return fmt.Errorf("sandbox: cannot determine base branch (set origin/HEAD or check out a branch)")
	}
	if dirty, _ := gitx.HasChanges(dir); dirty {
		return fmt.Errorf("sandbox: working tree is dirty — commit/stash before an isolated run, or pass --in-place to run directly in your tree (parking needs a clean tree)")
	}

	orig, _ := gitx.CurrentBranch(dir)
	park := "pact-sandbox-park"
	sbDir := filepath.Join(dir, ".pact", "orchestrate", sandboxDirName)

	if err := gitx.CheckoutOrCreate(dir, park); err != nil {
		return fmt.Errorf("sandbox: park main tree: %w", err)
	}
	// teardown removes the worktree and restores the user's branch. It runs BEFORE
	// the ledger is copied back, so the restore checkout sees a clean tree even when
	// .pact is tracked (the 回灌 would otherwise dirty it and block the checkout).
	teardown := func() {
		_ = gitx.RemoveWorktree(dir, sbDir)
		_ = os.RemoveAll(sbDir)
		if orig != "" {
			_ = gitx.Checkout(dir, orig)
		}
	}

	_ = gitx.RemoveWorktree(dir, sbDir)
	_ = os.RemoveAll(sbDir)
	if err := gitx.AddWorktree(dir, sbDir, base, park); err != nil {
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
	runErr := o.withDefaults().run(ctx)

	ledger := readLedger(sbDir) // capture the advanced ledger before the worktree goes
	teardown()                  // remove worktree + restore the main tree (still clean)
	writeLedger(dir, ledger)    // 回灌 onto the restored tree
	return runErr
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

// writeLedger restores a snapshot taken by readLedger into dir/.pact.
func writeLedger(dir string, m map[string][]byte) {
	for f, b := range m {
		_ = os.WriteFile(filepath.Join(dir, ".pact", f), b, 0o644)
	}
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
