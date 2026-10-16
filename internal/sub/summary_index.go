package sub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/agentjoey/pactify/internal/procx"
)

type resumeScope struct{ Cwd, Harness, Session string }
type resumeGroup struct{ latest, active string }

// SummaryIndex owns a single read-only snapshot for a list/detail response.
// Each state/result and process identity is inspected once, never per row/action.
type SummaryIndex struct {
	store         Store
	states        map[string]State
	results       map[string]Result
	resumedBy     map[string][]string
	scopes        map[string]resumeScope
	groups        map[resumeScope]resumeGroup
	worktreeBusy  map[string]error
	fallbackFinal map[string]string
}

// States reads atomic job snapshots without recovery or writes. Incomplete job
// directories are skipped; damaged existing state is still an error.
func (s Store) States() ([]State, error) { return s.readStates(false) }

func (s Store) readStates(skipInvalid bool) ([]State, error) {
	entries, err := os.ReadDir(s.Root)
	if os.IsNotExist(err) {
		return []State{}, nil
	}
	if err != nil {
		return nil, err
	}
	states := make([]State, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		st, err := s.Load(entry.Name())
		if os.IsNotExist(err) || skipInvalid && err != nil {
			continue
		}
		if err != nil {
			return nil, err
		}
		states = append(states, st)
	}
	return states, nil
}

func (s Store) NewSummaryIndex(states []State) *SummaryIndex {
	index := &SummaryIndex{store: s, states: map[string]State{}, results: map[string]Result{}, resumedBy: map[string][]string{}, scopes: map[string]resumeScope{}, groups: map[resumeScope]resumeGroup{}, worktreeBusy: map[string]error{}, fallbackFinal: map[string]string{}}
	for _, st := range states {
		index.states[st.ID] = st
		if st.ResumeOf != "" {
			index.resumedBy[st.ResumeOf] = append(index.resumedBy[st.ResumeOf], st.ID)
		}
		var result Result
		if data, err := os.ReadFile(s.ResultPath(st.ID)); err == nil {
			_ = json.Unmarshal(data, &result)
		}
		index.results[st.ID] = result
	}
	for id := range index.resumedBy {
		sort.Strings(index.resumedBy[id])
	}
	roots := map[string]string{}
	visiting := map[string]bool{}
	var root func(string) string
	root = func(id string) string {
		if r, ok := roots[id]; ok {
			return r
		}
		if visiting[id] {
			return ""
		}
		visiting[id] = true
		st := index.states[id]
		r := id
		if _, ok := index.states[st.ResumeOf]; ok && st.ResumeOf != "" {
			r = root(st.ResumeOf)
		}
		visiting[id] = false
		roots[id] = r
		return r
	}
	canonical := map[string]string{}
	for id, st := range index.states {
		rootID := root(id)
		if rootID == "" {
			continue
		}
		original := index.states[rootID]
		cwd, ok := canonical[original.Request.Cwd]
		if !ok {
			cwd = canonicalResumeCwd(original.Request.Cwd)
			canonical[original.Request.Cwd] = cwd
		}
		session := original.Request.Resume
		if session == "" {
			session = index.results[rootID].SessionID
		}
		if session == "" {
			session = "job:" + rootID
		}
		scope := resumeScope{Cwd: cwd, Harness: original.Request.Harness, Session: session}
		index.scopes[id] = scope
		group := index.groups[scope]
		if group.latest == "" || newerTurn(st, index.states[group.latest]) {
			group.latest = id
		}
		if !Terminal(st.Status) {
			group.active = id
		}
		index.groups[scope] = group
	}
	processBusy := map[procx.Identity]bool{}
	for _, st := range index.states {
		if st.Worktree == nil {
			continue
		}
		owner := st.Worktree.Owner
		if !Terminal(st.Status) {
			index.worktreeBusy[owner] = actionConflict("job %s is still running; run pactify sub cancel %s first", st.ID, st.ID)
			continue
		}
		for _, p := range append(append([]procx.Identity(nil), st.Processes...), st.Descendants...) {
			busy, known := processBusy[p]
			if !known {
				alive, err := p.Alive()
				busy = alive || err != nil
				processBusy[p] = busy
			}
			if busy {
				index.worktreeBusy[owner] = actionConflict("the harness for job %s may still be running; run pactify sub cancel %s first", st.ID, st.ID)
			}
		}
	}
	visiting = map[string]bool{}
	var final func(string) string
	final = func(id string) string {
		if f, ok := index.fallbackFinal[id]; ok {
			return f
		}
		if visiting[id] {
			return ""
		}
		st, ok := index.states[id]
		if !ok {
			return ""
		}
		visiting[id] = true
		f := id
		if st.FallbackTo != "" {
			f = final(st.FallbackTo)
		}
		visiting[id] = false
		index.fallbackFinal[id] = f
		return f
	}
	for id := range index.states {
		final(id)
	}
	return index
}

func newerTurn(a, b State) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	if !a.FinishedAt.Equal(b.FinishedAt) {
		return a.FinishedAt.After(b.FinishedAt)
	}
	return a.ID > b.ID
}
func canonicalResumeCwd(cwd string) string {
	if cwd == "" {
		return ""
	}
	if absolute, err := filepath.Abs(cwd); err == nil {
		cwd = absolute
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	return filepath.Clean(cwd)
}

func (s Store) actionIndex(st State) (*SummaryIndex, error) {
	states, err := s.readStates(true)
	if err != nil {
		return nil, err
	}
	found := false
	for i := range states {
		if states[i].ID == st.ID {
			states[i] = st
			found = true
			break
		}
	}
	if !found {
		states = append(states, st)
	}
	return s.NewSummaryIndex(states), nil
}

func (index *SummaryIndex) checkResumeIdle(st State) error {
	scope, ok := index.scopes[st.ID]
	if !ok {
		return actionConflict("resume chain contains a cycle")
	}
	if active := index.groups[scope].active; active != "" {
		return actionConflict("job %s is still running; wait for it to finish before resuming", active)
	}
	return nil
}
