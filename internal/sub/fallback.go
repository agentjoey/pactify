package sub

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/lockx"
)

// FinalJob follows only fallback links. Status/list/history retain each attempt.
func (s Store) FinalJob(id string) (State, error) { return s.finalJob(id, s.Recover) }

// FinalJobSnapshot follows fallback links without recovery or filesystem writes.
func (s Store) FinalJobSnapshot(id string) (State, error) { return s.finalJob(id, s.Load) }

func (s Store) finalJob(id string, load func(string) (State, error)) (State, error) {
	seen := map[string]bool{}
	for {
		if seen[id] {
			return State{}, fmt.Errorf("fallback cycle at job %s", id)
		}
		seen[id] = true
		st, err := load(id)
		if err != nil {
			return st, err
		}
		if st.FallbackTo == "" {
			return st, nil
		}
		id = st.FallbackTo
	}
}

// Called with the parent's state lock, after harness teardown and quota memory.
// Publish both links before the parent's terminal state becomes visible.
func (s Store) quotaFallbackLocked(st *State, result *Result, now time.Time) error {
	if st.Routing == nil || len(st.Routing.Remaining) == 0 || result.Status != StatusFailed || result.FailureClass != "quota" || st.CancelRequested || result.fallbackBlocked {
		return nil
	}
	r := &Router{QuotaGroups: st.Routing.QuotaGroups}
	quotas, err := s.ExhaustedRoutes(now)
	if err != nil {
		return err
	}
	next, remaining, warnings, err := r.Select(st.Routing.Remaining, quotas, st.Request.Access, now)
	if err != nil {
		result.Warnings = append(result.Warnings, err.Error())
		return nil
	}
	suggest := fmt.Sprintf("review the diff, then: pactify sub run %s", next.Harness)
	if next.Model != "" {
		suggest += " --model " + shellQuote(next.Model)
	}
	if next.Effort != "" {
		suggest += " --effort " + shellQuote(next.Effort)
	}
	suggest += " --worktree-from " + st.ID
	var diff Diff
	if st.Worktree != nil {
		var change worktreeChange
		change, err = worktreeChanges(st.Worktree, false)
		diff = change.diff
	} else if st.Baseline != nil {
		diff, err = DiffSince(st.Request.Cwd, *st.Baseline)
		// Check preexisting file fingerprints as well as attributed diff stats.
		if err == nil {
			root, e := repositoryRoot(st.Request.Cwd)
			if e != nil {
				return e
			}
			for path, want := range st.Baseline.Fingerprints {
				data, e := fileContent(filepath.Join(root, path))
				if e != nil || sha256.Sum256(data) != want {
					err = fmt.Errorf("preexisting file %s changed or is unavailable", path)
					break
				}
			}
		}
	} else {
		err = fmt.Errorf("baseline unavailable")
	}
	if err != nil || len(diff.Files) > 0 {
		if err != nil {
			result.Warnings = append(result.Warnings, "Cannot prove workspace unchanged: "+err.Error())
		}
		result.Warnings = append(result.Warnings, suggest)
		return nil
	}
	childID := NewJobID(now, rand.Reader)
	req := next.apply(st.Request)
	req.Resume = ""
	child, err := s.Create(childID, req)
	if err != nil {
		return err
	}
	child.FallbackFrom = st.ID
	child.Baseline = st.Baseline
	child.Routing = &Routing{Remaining: remaining, QuotaGroups: st.Routing.QuotaGroups}
	warning := quotaFallbackWarning(Route{Harness: st.Request.Harness, Model: st.Request.Model, Effort: st.Request.Effort}, next, result.QuotaResetAt, now)
	child.RouteWarnings = append(append(append([]string{}, st.RouteWarnings...), warning), warnings...)
	if st.Worktree != nil {
		copy := *st.Worktree
		copy.Holder = childID
		copy.Applied = false
		child.Worktree = &copy
		// The lock key remains the first job's Owner across transfers/resumes.
		st.Worktree.Holder = childID
		if st.Worktree.Owner != st.ID {
			if err = s.WithState(context.Background(), st.Worktree.Owner, func(owner *State) error { owner.Worktree.Holder = childID; return s.Save(*owner) }); err != nil {
				return err
			}
		}
	}
	if err = s.Save(child); err != nil {
		return err
	}
	st.FallbackTo = childID
	result.FallbackTo = childID
	result.Warnings = append(result.Warnings, warning)
	return nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

// WorktreeFrom starts a fresh session, transferring exclusive use while retaining
// the shared lock identity and original base (including earlier turns' edits).
func (s Store) WorktreeFrom(id string, req Request, selector string) (created State, err error) {
	previous, err := s.ResolveResumeJob(selector)
	if err != nil {
		return created, err
	}
	err = s.withWorktree(previous.ID, func(source *State, w *Worktree) error {
		if err := s.worktreeIdle(w); err != nil {
			return err
		}
		root, err := repositoryRoot(w.Cwd)
		if err != nil {
			return err
		}
		actual, err := filepath.EvalSymlinks(root)
		if err != nil {
			return err
		}
		expected, err := filepath.EvalSymlinks(w.Path)
		if err != nil {
			return err
		}
		if actual != expected {
			return fmt.Errorf("worktree path no longer points to the original isolated repository")
		}
		req.Resume = ""
		req.Cwd = source.Request.Cwd
		created, err = s.Create(id, req)
		if err != nil {
			return err
		}
		copy := *w
		copy.Applied = false
		copy.Holder = id
		created.Worktree = &copy
		created.Baseline = source.Baseline
		if err = s.Save(created); err != nil {
			return err
		}
		// Mark every prior turn under the same worktree lock. Old ids cannot launch,
		// apply or discard after transfer; their error points to the current holder.
		return s.transferHolder(w, id)
	})
	return
}
func (s Store) transferHolder(w *Worktree, id string) error {
	// Avoid re-locking a running parent: automatic fallback updates its own state
	// under Finish instead. Manual transfers happen only after all turns finish.
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == id {
			continue
		}
		st, err := s.Load(entry.Name())
		if err != nil || st.Worktree == nil || st.Worktree.Owner != w.Owner {
			continue
		}
		if err = s.WithState(context.Background(), st.ID, func(st *State) error { st.Worktree.Holder = id; return s.Save(*st) }); err != nil {
			return err
		}
	}
	return nil
}

// worktreeLease serializes fallback publication with apply/discard/resume.
func (s Store) worktreeLease(st State) (func(), error) {
	if st.Worktree == nil {
		return func() {}, nil
	}
	return lockx.Acquire(context.Background(), filepath.Join(s.Dir(st.Worktree.Owner), "worktree.lock"))
}
