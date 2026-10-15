package sub

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ResolveResumeJob prefers an exact id, otherwise matching the start of the id
// or its random suffix. Resolve across all jobs so a harness mismatch cannot
// silently select another job.
func (s Store) ResolveResumeJob(selector string) (State, error) {
	if err := validJobID(selector); err != nil {
		return State{}, err
	}
	st, err := s.Load(selector)
	if err == nil {
		return st, nil
	}
	if !os.IsNotExist(err) {
		return State{}, err
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil && !os.IsNotExist(err) {
		return State{}, err
	}
	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		suffix := id[strings.LastIndex(id, "-")+1:]
		if strings.HasPrefix(id, selector) || strings.HasPrefix(suffix, selector) {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 0:
		return State{}, fmt.Errorf("job %q not found", selector)
	case 1:
		return s.Load(matches[0])
	default:
		return State{}, fmt.Errorf("ambiguous job prefix %q; candidates:\n  %s", selector, strings.Join(matches, "\n  "))
	}
}

// ResumeRun resumes a particular terminal job rather than the latest session.
// An omitted harness inherits the selected job's harness.
func (s Store) ResumeRun(id string, req Request, isolated bool, selector string) (State, error) {
	previous, err := s.ResolveResumeJob(selector)
	if err != nil {
		return State{}, err
	}
	if req.Harness == "" {
		req.Harness = previous.Request.Harness
	}
	if req.Harness != previous.Request.Harness {
		return State{}, fmt.Errorf("job %s uses harness %s, not %s", previous.ID, previous.Request.Harness, req.Harness)
	}
	if !Terminal(previous.Status) {
		return State{}, fmt.Errorf("job %s is not finished (%s)", previous.ID, previous.Status)
	}
	session, err := s.resumeSession(previous)
	if err != nil {
		return State{}, err
	}
	if session == "" {
		return State{}, fmt.Errorf("job %s has no session id to resume", previous.ID)
	}
	req.Cwd = previous.Request.Cwd
	return s.resumeRun(id, req, isolated, previous, session)
}

func (s Store) resumeSession(st State) (string, error) {
	data, err := os.ReadFile(s.ResultPath(st.ID))
	if err != nil {
		return "", fmt.Errorf("job %s has no readable session id: %w", st.ID, err)
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("job %s has no readable session id: %w", st.ID, err)
	}
	return result.SessionID, nil
}

// ResumeCandidates lists usable sessions in the same lookup scope as automatic
// resume. Selection itself retains lastRunFor's legacy ordering and failures.
func (s Store) ResumeCandidates(harness, cwd string) ([]State, error) {
	entries, err := os.ReadDir(s.Root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var candidates []State
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		st, err := s.Load(entry.Name())
		if err != nil || st.Request.Harness != harness || st.Request.Cwd != cwd {
			continue
		}
		if st.Status != StatusSucceeded && (st.Worktree == nil || !Terminal(st.Status)) {
			continue
		}
		if session, err := s.resumeSession(st); err != nil || session == "" {
			continue
		}
		if st.Worktree != nil {
			owner, err := s.Load(st.Worktree.Owner)
			if err != nil || owner.Worktree == nil || owner.Worktree.Discarded {
				continue
			}
			if _, err := os.Stat(owner.Worktree.Cwd); err != nil {
				continue
			}
		}
		candidates = append(candidates, st)
	}
	return candidates, nil
}
