package sub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/agentjoey/pactify/internal/procx"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/lockx"
)

// Worktree is shared by every turn of a resumed session. Owner names the first
// job, whose worktree.lock serializes resume, apply and discard across turns.
type Worktree struct {
	Holder       string `json:"holder,omitempty"` // Current session owner after a cross-harness transfer; Owner stays the shared lock key.
	Applied      bool   `json:"applied,omitempty"`
	Path         string `json:"path"`
	BaseCommit   string `json:"baseCommit"`
	OriginalRoot string `json:"originalRoot"`
	Cwd          string `json:"cwd"`
	Owner        string `json:"owner"`
	SourceDirty  bool   `json:"sourceDirty"`
	Discarded    bool   `json:"discarded"`
}

const dirtyWorktreeWarning = "The original workspace has uncommitted changes; they were not included in the worktree"

// ErrWorktreeNeedsCommit rejects isolation before any job is persisted.
var ErrWorktreeNeedsCommit = errors.New("--worktree needs a base commit; commit once first (the repository has no commits yet)")

// NoSessionError is a request error, rather than a persistence/startup failure.
type NoSessionError struct{ Harness string }

func (e *NoSessionError) Error() string {
	return fmt.Sprintf("This repository has no %s session to resume", e.Harness)
}

// ReadOnlyUnavailableError also rejects legacy read-only sessions on resume.
type ReadOnlyUnavailableError struct{ Reason string }

func (e *ReadOnlyUnavailableError) Error() string { return e.Reason }

// CreateRun keeps the original cwd as the session lookup key. A worktree resume
// automatically inherits isolation, even when --worktree is omitted.
func (s Store) CreateRun(id string, req Request, isolated, resume bool) (State, error) {
	if !resume && !isolated {
		return s.Create(id, req)
	}
	if resume {
		previous, session, err := s.lastRunFor(req.Harness, req.Cwd)
		if err != nil {
			return State{}, err
		}
		if session == "" {
			return State{}, &NoSessionError{Harness: req.Harness}
		}
		return s.resumeRun(id, req, isolated, previous, session)
	}
	root, err := repositoryRoot(req.Cwd)
	if err != nil {
		return State{}, err
	}
	base, err := headCommit(root)
	if err != nil {
		return State{}, err
	}
	if base == "" {
		return State{}, ErrWorktreeNeedsCommit
	}
	dirty, err := runGit(root, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return State{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return State{}, err
	}
	// Let git place the cwd in the repository: comparing path strings fails
	// when the cwd is spelled differently from git's root, as on
	// case-insensitive macOS volumes (~/agentworks vs ~/AgentWorks).
	prefix, err := runGit(req.Cwd, "rev-parse", "--show-prefix")
	if err != nil {
		return State{}, err
	}
	rel := filepath.Clean(filepath.FromSlash(strings.TrimSuffix(string(prefix), "\n")))
	if !filepath.IsLocal(rel) {
		return State{}, fmt.Errorf("cwd is outside the worktree")
	}
	st, err := s.Create(id, req)
	if err != nil {
		return State{}, err
	}
	path, err := filepath.Abs(filepath.Join(s.Dir(id), "wt"))
	if err != nil {
		return State{}, err
	}
	w := &Worktree{Path: path, Cwd: filepath.Join(path, rel), OriginalRoot: root, BaseCommit: base, Owner: id, SourceDirty: len(dirty) > 0}
	// Hold the lifecycle lock through checkout: Recover must not declare a slow
	// preparation dead or publish a terminal result while git is still writing.
	err = s.WithState(context.Background(), id, func(current *State) error {
		if Terminal(current.Status) || current.CancelRequested {
			return context.Canceled
		}
		current.Worktree = w
		if err := s.Save(*current); err != nil {
			return err
		}
		_, setupErr := runGit(root, "worktree", "add", "--detach", path, w.BaseCommit)
		if setupErr == nil {
			// Empty/untracked source directories may not exist at HEAD.
			setupErr = makeWorktreeCwd(w, rel)
		}
		current.UpdatedAt = time.Now().UTC()
		saveErr := s.Save(*current)
		st = *current
		return errors.Join(setupErr, saveErr)
	})
	if err != nil {
		r := Result{JobID: id, Status: StatusFailed, Harness: req.Harness, Access: req.Access, Warnings: []string{err.Error()}}
		if finishErr := s.Finish(id, &r, err.Error(), time.Now()); finishErr != nil {
			return st, fmt.Errorf("%v; save failure: %w", err, finishErr)
		}
	}
	return st, err
}

func (s Store) resumeRun(id string, req Request, isolated bool, previous State, session string) (State, error) {
	h, _ := Lookup(req.Harness)
	if h.NoReadOnly != "" && (req.Access == "read-only" || previous.Request.Access == "read-only") {
		return State{}, &ReadOnlyUnavailableError{Reason: h.NoReadOnly}
	}
	if h.Home != nil {
		if previous.EffectiveConfigMode() != h.configMode(req.WithUserConfig) {
			return State{}, fmt.Errorf("%s resume requires the original config mode (%s); use the matching --with-user-config setting or start a fresh session", h.Name, previous.EffectiveConfigMode())
		}
	}
	return s.withResumeLock(previous, func(index *SummaryIndex) (State, error) {
		if req.StartedFrom == "dashboard" {
			if err := index.CheckAction(previous, "resume"); err != nil {
				return State{}, err
			}
		}
		return s.resumeRunLocked(id, req, isolated, previous, session)
	})
}

func (s Store) resumeRunLocked(id string, req Request, isolated bool, previous State, session string) (State, error) {
	h, _ := Lookup(req.Harness)
	createResume := func() (State, error) {
		st, err := s.Create(id, req)
		if err != nil {
			return st, err
		}
		err = s.WithState(context.Background(), id, func(current *State) error {
			current.ResumeOf = previous.ID
			if h.Home != nil {
				current.HarnessHome = previous.HarnessHome
				current.HarnessHomes = previous.HarnessHomes
			}
			st = *current
			return s.Save(*current)
		})
		return st, err
	}
	req.Resume = session
	if previous.Worktree == nil {
		if isolated {
			return State{}, fmt.Errorf("The latest session did not use a worktree; remove --resume to create an isolated session")
		}
		return createResume()
	}
	var created State
	err := s.withWorktree(previous.ID, func(_ *State, w *Worktree) error {
		if err := s.worktreeIdle(w); err != nil {
			return err
		}
		root, err := repositoryRoot(w.Cwd)
		if err != nil {
			return fmt.Errorf("worktree no longer exists; cannot resume: %w", err)
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
		created, err = createResume()
		if err != nil {
			return err
		}
		err = s.WithState(context.Background(), id, func(st *State) error {
			copy := *w
			copy.Applied = false
			if copy.Holder != "" {
				copy.Holder = id
			}
			st.Worktree = &copy
			created = *st
			return s.Save(*st)
		})
		if err == nil && w.Holder != "" {
			err = s.transferHolder(w, id)
		}
		return err
	})
	return created, err
}

func makeWorktreeCwd(w *Worktree, rel string) error {
	if !filepath.IsLocal(rel) {
		return fmt.Errorf("cwd is outside the worktree")
	}
	path := w.Path
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		path = filepath.Join(path, component)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		// A dirty source may have replaced a HEAD symlink with a directory.
		// Following that symlink here would run the callee outside isolation.
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("worktree cwd contains a symlink in HEAD: %s", path)
		}
	}
	return os.MkdirAll(w.Cwd, 0700)
}

func (s Store) lastRunFor(harness, cwd string) (State, string, error) {
	entries, err := os.ReadDir(s.Root)
	if os.IsNotExist(err) {
		return State{}, "", nil
	}
	if err != nil {
		return State{}, "", err
	}
	var latest State
	var session string
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
		if latest.ID != "" && !st.FinishedAt.After(latest.FinishedAt) {
			continue
		}
		data, err := os.ReadFile(s.ResultPath(st.ID))
		var result Result
		if err != nil || json.Unmarshal(data, &result) != nil {
			continue
		}
		if st.Worktree != nil && result.SessionID == "" {
			continue
		}
		latest, session = st, result.SessionID
	}
	return latest, session, nil
}

func (s Store) withWorktree(id string, f func(*State, *Worktree) error) error {
	st, err := s.Recover(id)
	if err != nil {
		return err
	}
	if st.Worktree == nil {
		return actionConflict("job %s did not use a worktree", id)
	}
	if err := validJobID(st.Worktree.Owner); err != nil {
		return err
	}
	release, err := lockx.Acquire(context.Background(), filepath.Join(s.Dir(st.Worktree.Owner), "worktree.lock"))
	if err != nil {
		return err
	}
	defer release()
	// Reload after taking the shared lock; discard/resume may have won the race.
	st, err = s.Load(id)
	if err != nil {
		return err
	}
	owner, err := s.Load(st.Worktree.Owner)
	if err != nil {
		return err
	}
	if owner.Worktree == nil || owner.Worktree.Discarded {
		return actionConflict("worktree has been discarded and cannot be reused")
	}
	holder := owner.Worktree.Holder
	if holder != "" && holder != id {
		return actionConflict("worktree transferred to job %s; use that job id", holder)
	}
	return f(&st, st.Worktree)
}

func (s Store) worktreeIdle(w *Worktree) error {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		st, err := s.Load(entry.Name())
		if err != nil || st.Worktree == nil || st.Worktree.Owner != w.Owner {
			continue
		}
		st, err = s.Recover(st.ID)
		if err != nil {
			return err
		}
		if !Terminal(st.Status) {
			return actionConflict("job %s is still running; run pactify sub cancel %s first", st.ID, st.ID)
		}
		// Terminal output alone does not guarantee that every harness exited.
		for _, p := range append(append([]procx.Identity(nil), st.Processes...), st.Descendants...) {
			alive, err := p.Alive()
			if err != nil {
				return err
			}
			if alive {
				return actionConflict("the harness for job %s is still running; run pactify sub cancel %s first", st.ID, st.ID)
			}
		}
	}
	return nil
}

// worktreeChanges uses an isolated index: include the full net change from the
// original base, including commits, unstaged files, and untracked additions.
// Neither the callee's index nor the user's index is changed.
type worktreeChange struct {
	patch []byte
	paths []byte
	diff  Diff
}

func worktreeChanges(w *Worktree, includePatch bool) (change worktreeChange, err error) {
	f, err := os.CreateTemp("", "pactify-sub-index-*")
	if err != nil {
		return change, err
	}
	index := f.Name()
	if err = f.Close(); err != nil {
		return change, err
	}
	defer os.Remove(index)
	defer os.Remove(index + ".lock")
	if err = os.Remove(index); err != nil {
		return change, err
	}
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", append([]string{"-c", "core.filemode=true"}, args...)...)
		cmd.Dir = w.Path
		cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+index)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
		}
		return out, nil
	}
	if _, err = run("read-tree", "HEAD"); err != nil {
		return change, err
	}
	if _, err = run("add", "-A", "--", "."); err != nil {
		return change, err
	}
	change.paths, err = run("diff", "--cached", "--name-only", "-z", "--no-renames", w.BaseCommit, "--")
	if err != nil {
		return change, err
	}
	stats, err := run("diff", "--cached", "--numstat", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", w.BaseCommit, "--")
	if err != nil {
		return change, err
	}
	added, err := run("diff", "--cached", "--name-only", "-z", "--diff-filter=A", "--no-renames", w.BaseCommit, "--")
	if err != nil {
		return change, err
	}
	newPaths := make(map[string]bool)
	for _, path := range bytes.Split(added, []byte{0}) {
		newPaths[string(path)] = true
	}
	for _, record := range bytes.Split(stats, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		f, err := parseNumstat(record)
		if err != nil {
			return change, err
		}
		f.New = newPaths[f.Path]
		change.diff.Files = append(change.diff.Files, f)
		change.diff.Added += f.Added
		change.diff.Deleted += f.Deleted
	}
	if includePatch {
		change.patch, err = run("diff", "--cached", "--binary", "--full-index", "--no-renames", "--no-ext-diff", "--no-textconv", w.BaseCommit, "--")
	}
	return change, err
}

func (s Store) ApplyWorktree(id string) error {
	return s.withWorktree(id, func(st *State, w *Worktree) error {
		if err := s.worktreeIdle(w); err != nil {
			return err
		}
		if err := s.CheckAction(*st, "apply"); err != nil {
			return err
		}
		change, err := worktreeChanges(w, true)
		if err != nil {
			return err
		}
		if len(change.patch) == 0 {
			return s.markWorktreeApplied(w)
		}
		// Check both index and working tree against the base. A staged edit
		// restored only in the working tree is still a user change.
		for _, path := range bytes.Split(change.paths, []byte{0}) {
			if len(path) == 0 {
				continue
			}
			for _, cached := range []bool{false, true} {
				args := []string{"-c", "core.filemode=true", "--literal-pathspecs", "diff", "--quiet", "--no-ext-diff", "--no-textconv"}
				if cached {
					args = append(args, "--cached")
				}
				args = append(args, w.BaseCommit, "--", string(path))
				if _, err := runGit(w.OriginalRoot, args...); err != nil {
					return actionConflict("affected file %q in the original workspace has changed; not applied: %v", path, err)
				}
			}
		}
		apply := func(check bool) error {
			args := []string{"apply", "--binary", "--whitespace=nowarn"}
			if check {
				args = append(args, "--check")
			}
			cmd := exec.Command("git", args...)
			cmd.Dir = w.OriginalRoot
			cmd.Stdin = bytes.NewReader(change.patch)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return actionConflict("apply failed: %v: %s", err, out)
			}
			return nil
		}
		if err := apply(true); err != nil {
			return err
		}
		// git apply without --reject validates the whole patch before writing.
		if err := apply(false); err != nil {
			return err
		}
		return s.markWorktreeApplied(w)
	})
}

func (s Store) DiscardWorktree(id string) error {
	return s.withWorktree(id, func(st *State, w *Worktree) error {
		if err := s.worktreeIdle(w); err != nil {
			return err
		}
		if err := s.CheckAction(*st, "discard"); err != nil {
			return err
		}
		entries, err := os.ReadDir(s.Root)
		if err != nil {
			return err
		}
		var related []State
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			st, err := s.Load(entry.Name())
			if err != nil || st.Worktree == nil || st.Worktree.Owner != w.Owner {
				continue
			}
			if err := st.KillHarnesses(); err != nil {
				return err
			}
			related = append(related, st)
		}
		// git also removes registrations for a worktree whose directory was
		// deleted externally. Never prune other jobs' worktrees.
		if _, err := runGit(w.OriginalRoot, "worktree", "remove", "--force", "--force", w.Path); err != nil {
			if _, statErr := os.Lstat(w.Path); !os.IsNotExist(statErr) {
				return err
			}
			list, listErr := runGit(w.OriginalRoot, "worktree", "list", "--porcelain", "-z")
			if listErr != nil {
				return listErr
			}
			parent, pathErr := filepath.EvalSymlinks(filepath.Dir(w.Path))
			if pathErr != nil {
				return pathErr
			}
			expected := filepath.Join(parent, filepath.Base(w.Path))
			for _, field := range bytes.Split(list, []byte{0}) {
				if string(field) == "worktree "+expected {
					return err
				}
			}
		}
		for _, st := range related {
			if err := s.cleanupJobTemp(st.ID); err != nil {
				return err
			}
			if err := s.WithState(context.Background(), st.ID, func(current *State) error {
				current.Worktree.Discarded = true
				current.UpdatedAt = time.Now().UTC()
				return s.Save(*current)
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Result) describeWorktree(st State) {
	if st.Worktree == nil {
		return
	}
	r.WorktreePath, r.BaseCommit = st.Worktree.Path, st.Worktree.BaseCommit
	if st.Worktree.SourceDirty {
		r.Warnings = append([]string{dirtyWorktreeWarning}, r.Warnings...)
	}
}

// markWorktreeApplied records a successful apply for every existing turn sharing
// this worktree. The caller holds the owner worktree lock; a new turn resets its
// own marker because it can introduce further changes.
func (s Store) markWorktreeApplied(w *Worktree) error {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		st, err := s.Load(entry.Name())
		if err != nil || st.Worktree == nil || st.Worktree.Owner != w.Owner {
			continue
		}
		if err := s.WithState(context.Background(), st.ID, func(current *State) error {
			current.Worktree.Applied = true
			return s.Save(*current)
		}); err != nil {
			return err
		}
	}
	return nil
}
