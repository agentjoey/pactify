package sub

import (
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"time"
)

// ActionConflict is a state precondition failure, suitable for HTTP 409.
type ActionConflict struct{ Message string }

func (e *ActionConflict) Error() string { return e.Message }
func actionConflict(format string, args ...any) error {
	return &ActionConflict{Message: fmt.Sprintf(format, args...)}
}

// CheckAction is read-only: summaries and HTTP mutations use the same rules.
// The mutation primitives still recheck lifecycle/worktree state under their locks.
func (s Store) CheckAction(st State, action string) error {
	index, err := s.actionIndex(st)
	if err != nil {
		return err
	}
	return index.CheckAction(st, action)
}
func (s Store) Actions(st State) []string {
	index, err := s.actionIndex(st)
	if err != nil {
		return []string{}
	}
	return index.Actions(st)
}
func (index *SummaryIndex) Actions(st State) []string {
	actions := []string{}
	for _, action := range []string{"cancel", "resume", "apply", "discard", "dismiss"} {
		if index.CheckAction(st, action) == nil {
			actions = append(actions, action)
		}
	}
	return actions
}
func (index *SummaryIndex) CheckAction(st State, action string) error {
	if action == "cancel" {
		if st.FallbackTo != "" {
			id := index.fallbackFinal[st.ID]
			if id == "" {
				return actionConflict("cannot resolve current fallback job")
			}
			st = index.states[id]
		}
		if Terminal(st.Status) {
			return actionConflict("job %s is already finished (%s)", st.ID, st.Status)
		}
		if st.CancelRequested {
			return actionConflict("job %s is already being canceled", st.ID)
		}
		return nil
	}
	if action != "resume" && action != "apply" && action != "discard" && action != "dismiss" {
		return actionConflict("unknown job action %q", action)
	}
	if !Terminal(st.Status) {
		return actionConflict("job %s is still running; run pactify sub cancel %s first", st.ID, st.ID)
	}
	if action == "dismiss" {
		if !st.DismissedAt.IsZero() {
			return actionConflict("job %s is already dismissed", st.ID)
		}
		if st.FallbackTo != "" {
			return actionConflict("job %s was replaced by fallback job %s", st.ID, st.FallbackTo)
		}
		if w := st.Worktree; w != nil && (w.Applied || w.Discarded) {
			return actionConflict("job %s was already acknowledged by applying or discarding its worktree", st.ID)
		}
		if w := st.Worktree; w != nil {
			if err := index.worktreeBusy[w.Owner]; err != nil {
				return err
			}
		}
		// A pending worktree needs an exit too: its delegating session may be gone.
		if st.Worktree == nil && st.Status != StatusFailed && actionableWarnings(index.results[st.ID]) == 0 {
			return actionConflict("job %s has no failure or warnings to dismiss", st.ID)
		}
		return nil
	}
	if action == "resume" {
		session := index.results[st.ID].SessionID
		if session == "" {
			return actionConflict("job %s has no readable session id to resume", st.ID)
		}
		if err := index.checkResumeIdle(st); err != nil {
			return err
		}
		if latest := index.groups[index.scopes[st.ID]].latest; latest != st.ID {
			return actionConflict("resume is only available on the latest turn (%s)", latest)
		}

		h, ok := Lookup(st.Request.Harness)
		if !ok {
			return actionConflict("unknown harness %q", st.Request.Harness)
		}
		if h.NoReadOnly != "" && st.Request.Access == "read-only" {
			return actionConflict("%s", h.NoReadOnly)
		}
	}
	if st.Worktree == nil {
		if action == "resume" {
			return nil
		}
		return actionConflict("job %s did not use a worktree", st.ID)
	}
	w := st.Worktree
	owner, ok := index.states[w.Owner]
	if !ok || owner.Worktree == nil {
		return actionConflict("worktree owner is unavailable")
	}
	if w.Discarded || owner.Worktree.Discarded {
		return actionConflict("worktree has been discarded and cannot be reused")
	}
	if holder := owner.Worktree.Holder; holder != "" && holder != st.ID {
		return actionConflict("worktree transferred to job %s; use that job id", holder)
	}
	if action == "apply" && w.Applied {
		return actionConflict("worktree has already been applied")
	}
	path := w.Path
	if action == "resume" {
		path = w.Cwd
	}
	if _, err := os.Stat(path); action != "discard" && err != nil {
		return actionConflict("worktree no longer exists: %v", err)
	}
	if err := index.worktreeBusy[w.Owner]; err != nil {
		return err
	}
	return nil
}

func (s Store) resumeJob(selector, brief string, dashboard bool) (State, error) {
	if strings.TrimSpace(brief) == "" {
		return State{}, fmt.Errorf("task brief must not be empty")
	}
	previous, err := s.ResolveResumeJob(selector)
	if err != nil {
		return State{}, err
	}
	if err := s.CheckAction(previous, "resume"); err != nil {
		return State{}, err
	}
	req := previous.Request
	req.StartedFrom = ""
	if dashboard {
		req.StartedFrom = "dashboard"
	}
	req.Brief = brief
	// Legacy records without configMode used user configuration.
	req.WithUserConfig = previous.EffectiveConfigMode() == "user"
	return s.ResumeRun(NewJobID(time.Now(), rand.Reader), req, false, previous.ID)
}
