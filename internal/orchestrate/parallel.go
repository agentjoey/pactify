package orchestrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/projection"
)

// ParallelOptions wraps Options with a concurrency cap and a worktree root.
// MaxConcurrency <= 1 runs the serial driver. Park is the throwaway branch the
// primary tree sits on for the run's duration so the base branch stays free for
// each feature's merge (only one worktree can hold base at a time).
type ParallelOptions struct {
	Options
	MaxConcurrency int
	WorktreeRoot   string // default <Dir>/.pact/orchestrate/wt
	Park           string // default pact-parallel-park
}

// RunParallel drives every feature with runnable work concurrently — feature-level
// parallelism. Each feature advances in its own git worktree (isolated checkout of
// its branch), up to MaxConcurrency at once. Merges are serialized onto the base
// branch: the primary tree parks off base for the run so each worktree can, under
// a lock, run the ordinary pact merge (checkout base → merge → event) one at a
// time. A feature that escalates pauses only itself; the rest keep going.
func RunParallel(ctx context.Context, popts ParallelOptions) error {
	opts := popts.withDefaults()
	if popts.MaxConcurrency <= 1 {
		return opts.run(ctx)
	}

	root := popts.WorktreeRoot
	if root == "" {
		root = filepath.Join(opts.Dir, ".pact", "orchestrate", "wt")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("orchestrate: worktree root: %w", err)
	}
	park := popts.Park
	if park == "" {
		park = "pact-parallel-park"
	}

	st, err := pact.At(opts.Dir).StateProjection()
	if err != nil {
		return fmt.Errorf("orchestrate: read state: %w", err)
	}
	base, err := gitx.CurrentBranch(opts.Dir)
	if err != nil || base == "" {
		return fmt.Errorf("orchestrate: resolve base branch: %w", err)
	}

	// Independent features branch from the same base, so merging the second one
	// conflicts on the pact ledger files (each appended its own events to a shared
	// base). Resolve this structurally: a union merge driver on log.jsonl keeps
	// every side's events (the correct union, since state is recomputed from the
	// log), and on STATE.yml avoids a conflict on the throwaway projection cache
	// (StateProjection recomputes it from the log, never reads the file). Commit
	// the .gitattributes to base so worktrees inherit it and the merge honors it.
	if err := ensureUnionAttrs(opts.Dir); err != nil {
		return fmt.Errorf("orchestrate: ensure union merge attrs: %w", err)
	}

	// Start from a clean per-feature status dir so the dashboard doesn't show a
	// previous run's features.
	clearParallelStatus(opts.Dir)

	// Park the primary tree off base so worktrees can take base for their merges.
	if err := gitx.CheckoutOrCreate(opts.Dir, park); err != nil {
		return fmt.Errorf("orchestrate: park primary tree: %w", err)
	}
	defer func() {
		_ = gitx.Checkout(opts.Dir, base) // restore base (now carrying the merges)
	}()

	type result struct {
		feature   string
		worktree  string
		mergeable bool
		escalated bool
		err       error
	}
	inflight := map[string]string{} // feature -> worktree path
	done := map[string]bool{}       // feature -> shipped or escalated (terminal)
	results := make(chan result)
	var mergeMu sync.Mutex
	var dispatchErr error

	for {
		if err := ctx.Err(); err != nil {
			dispatchErr = err
			break
		}
		// Dispatch pending features (have runnable work, not in flight, not done),
		// up to the concurrency cap.
		for _, f := range st.Features {
			if len(inflight) >= popts.MaxConcurrency {
				break
			}
			if f.Status == "shipped" || done[f.ID] || inflight[f.ID] != "" {
				continue
			}
			if _, ok := featureAction(f, History{}, opts.Th); !ok {
				continue
			}
			wt := filepath.Join(root, f.ID)
			branch := f.Branch
			if branch == "" {
				branch = f.ID
			}
			if err := gitx.AddWorktree(opts.Dir, wt, branch, park); err != nil {
				dispatchErr = fmt.Errorf("orchestrate: add worktree %s: %w", f.ID, err)
				break
			}
			inflight[f.ID] = wt
			go func(feature, wt string) {
				m, e, err := opts.driveFeature(ctx, wt, feature)
				results <- result{feature: feature, worktree: wt, mergeable: m, escalated: e, err: err}
			}(f.ID, wt)
		}
		if dispatchErr != nil {
			break
		}
		if len(inflight) == 0 {
			break // nothing in flight and nothing dispatchable → finished
		}

		r := <-results
		delete(inflight, r.feature)
		done[r.feature] = true // terminal unless we re-open it below

		switch {
		case r.err != nil:
			_ = gitx.RemoveWorktree(opts.Dir, r.worktree)
			dispatchErr = r.err
		case r.escalated:
			_ = gitx.RemoveWorktree(opts.Dir, r.worktree)
		case r.mergeable:
			// Serialize merges: only one worktree holds base at a time.
			mergeMu.Lock()
			merr := opts.mergeFromWorktree(ctx, r.worktree, r.feature)
			mergeMu.Unlock()
			_ = gitx.RemoveWorktree(opts.Dir, r.worktree)
			if merr != nil {
				dispatchErr = merr
			} else {
				// Mark the feature shipped in the aggregated view.
				_ = writeFeatureStatus(opts.Dir, r.feature, Status{
					Feature: r.feature, Action: "done", Phase: "done", Done: true,
					UpdatedAt: statusNow(opts.Now),
				})
			}
		}
		if dispatchErr != nil {
			break
		}
		// Refresh state for the next dispatch decision (merged features now shipped
		// in base; but we read base via the parked primary tree, which doesn't see
		// them — done[] tracks terminal features, so a refreshed read is only needed
		// to pick up any newly-actionable features, which the static feature set
		// already covers).
	}

	// Drain any still-running goroutines so we don't leak them or their worktrees.
	for len(inflight) > 0 {
		r := <-results
		delete(inflight, r.feature)
		_ = gitx.RemoveWorktree(opts.Dir, r.worktree)
	}
	return dispatchErr
}

// unionAttrs are the merge rules that make concurrent feature merges conflict-free
// on the pact ledger. log.jsonl is the source of truth (append-only events) →
// union keeps all sides' events; STATE.yml is a recomputed projection → union
// keeps the merge non-conflicting (its content is irrelevant, recomputed from the
// log on the next read).
const unionAttrs = ".pact/log.jsonl merge=union\n.pact/STATE.yml merge=union\n"

// runtimeIgnoreMarker is the .gitignore entry covering every per-run artifact
// orchestrate writes under .pact/orchestrate/ (status.json, escalation records,
// and per-task stream logs). Ignoring the dir keeps these out of the user's
// `git status` so an agent's `git add -A` during verify can't sweep them up.
const runtimeIgnoreMarker = ".pact/orchestrate/"

// ensureRuntimeExcludedLocal makes the repo ignore .pact/orchestrate/ runtime
// artifacts via .git/info/exclude — a per-clone, NEVER-committed ignore file.
// Unlike the old ensureRuntimeIgnored (which committed .gitignore to the active
// branch and could move base out from under a concurrent writer — linx bcf9bf8;
// see spec coordination-authority P0a), this writes nothing to any tracked branch,
// so the driver is no longer a second writer to base just for runtime hygiene.
// Idempotent: a clone already carrying the entry is left untouched. Both the
// single-run (loop.run) and parallel (RunParallel via ensureUnionAttrs) paths
// rely on it before writing any runtime file.
func ensureRuntimeExcludedLocal(dir string) error {
	excl, err := gitx.GitPath(dir, "info/exclude")
	if err != nil {
		return err
	}
	_, err = ensureFileContains(excl, runtimeIgnoreMarker, runtimeIgnoreMarker+"\n")
	return err
}

// ensureUnionAttrs makes the repo safe for concurrent feature merges. Two things:
//   - .gitattributes union rules on the pact ledger, committed to the (base) branch
//     so feature branches off base inherit them — they MUST be tracked to govern how
//     concurrent merges fold log.jsonl/STATE.yml; and
//   - the runtime ignore for .pact/orchestrate/, routed through .git/info/exclude
//     (local, never committed) so this setup never lands a .gitignore commit on base.
//
// Idempotent: a repo already carrying the union attrs is left untouched (no empty
// commit).
func ensureUnionAttrs(dir string) error {
	if err := ensureRuntimeExcludedLocal(dir); err != nil {
		return err
	}
	changed, err := ensureFileContains(filepath.Join(dir, ".gitattributes"), "log.jsonl merge=union", unionAttrs)
	if err != nil || !changed {
		return err
	}
	return gitx.CommitPaths(dir, "chore(pact): union merge driver for the pact ledger (parallel orchestration)",
		".gitattributes")
}

// ensureFileContains makes sure path contains marker, appending block (or creating
// the file) when it does not. Reports whether it wrote anything.
func ensureFileContains(path, marker, block string) (bool, error) {
	cur, err := os.ReadFile(path)
	switch {
	case err == nil && strings.Contains(string(cur), marker):
		return false, nil
	case err == nil:
		return true, os.WriteFile(path, append(cur, []byte("\n"+block)...), 0o644)
	case os.IsNotExist(err):
		return true, os.WriteFile(path, []byte(block), 0o644)
	default:
		return false, err
	}
}

// driveFeature advances ONE feature in worktreeDir until all its tasks are
// accepted (mergeable=true) or it escalates (escalated=true). It runs
// owner/reviewer agents via the injected runner; it does NOT merge — the
// coordinator serializes merges. History is per-feature (task ids are unique), so
// no cross-feature locking is needed.
func (opts Options) driveFeature(ctx context.Context, worktreeDir, feature string) (mergeable bool, escalated bool, err error) {
	o := opts
	o.Dir = worktreeDir
	o.Feature = feature
	h := History{Rework: map[string]int{}, Fails: map[string]int{}}
	// Per-feature status goes to the PRIMARY tree (opts.Dir) so the dashboard can
	// aggregate all concurrent features; o.Dir is the isolated worktree.
	now := func() string { return statusNow(opts.Now) }
	emit := func(s Status) { _ = writeFeatureStatus(opts.Dir, feature, s) }

	for {
		if err := ctx.Err(); err != nil {
			return false, false, err
		}
		st, err := pact.At(o.Dir).StateProjection()
		if err != nil {
			return false, false, fmt.Errorf("orchestrate: read state (%s): %w", feature, err)
		}
		view := o.filtered(st)
		var f *projection.Feature
		for i := range view.Features {
			if view.Features[i].ID == feature {
				f = &view.Features[i]
			}
		}
		if f == nil {
			return false, false, nil
		}
		act, ok := featureAction(*f, h, o.Th)
		if !ok {
			return false, false, nil
		}
		if act.Kind == ActRunOwner || act.Kind == ActRunReviewer {
			if reason, isTripped := tripped(act.Task, h, o.Th); isTripped {
				emit(buildEscalatedStatus(view, act.Task, reason, h, now))
				return false, true, o.escalate(act.Task, reason, evidenceFor(st, act.Task),
					"人工介入后 pactify orchestrate 续跑")
			}
		}
		if o.Th.MaxIters > 0 && h.Iters >= o.Th.MaxIters {
			emit(buildEscalatedStatus(view, "", "iteration limit exceeded", h, now))
			return false, true, o.escalate("", "iteration limit exceeded", "(global cap)",
				"放宽 --max-iters 或检查为何 task 图迟迟不收敛")
		}

		emit(buildLoopStatus(view, act, h, now))
		switch act.Kind {
		case ActMerge:
			return true, false, nil
		case ActRunOwner:
			if err := o.runOwner(ctx, st, &h, act); err != nil {
				return false, false, err
			}
		case ActRunReviewer:
			if err := o.runReviewer(ctx, st, &h, act); err != nil {
				return false, false, err
			}
		default:
			return false, false, nil
		}
		h.Iters++
	}
}

// mergeFromWorktree runs the hard gate in the feature's worktree (which holds the
// accepted work on its branch), then merges via the ordinary pact merge executed
// FROM that worktree. The worktree checks out base (free because the primary tree
// is parked and this runs under the coordinator's merge lock), merges the feature
// branch, and appends the merge event — exactly the serial merge path, just
// relocated to the worktree so base contention is serialized.
func (opts Options) mergeFromWorktree(ctx context.Context, worktreeDir, feature string) error {
	st, err := pact.At(worktreeDir).StateProjection()
	if err != nil {
		return fmt.Errorf("orchestrate: read worktree state (%s): %w", feature, err)
	}
	var feat *projection.Feature
	for i := range st.Features {
		if st.Features[i].ID == feature {
			feat = &st.Features[i]
		}
	}
	if feat == nil {
		return fmt.Errorf("orchestrate: feature %s not found in worktree for merge", feature)
	}
	for _, cmd := range gateCommands(worktreeDir, *feat) {
		ok, detail := runGate(ctx, opts.Exec, worktreeDir, cmd)
		if !ok {
			o := opts
			o.Dir = worktreeDir
			return o.escalate(feature, "hard gate failed: "+detail,
				evidenceFor(st, ""), "修复实现/规格后 pactify orchestrate 续跑")
		}
	}
	if err := pact.At(worktreeDir).As(opts.Orchestrator).Merge(feature); err != nil {
		return fmt.Errorf("orchestrate: merge %s from worktree: %w", feature, err)
	}
	// pact.Merge now commits the merge event + shipped STATE itself, so this
	// worktree's HEAD already carries the shipped state before we discard it.
	// (The old explicit CommitAll here is gone — it would now be a redundant second
	// commit and fail with nothing-to-commit.)
	return nil
}
