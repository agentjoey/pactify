package sub

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type Snapshot struct {
	Base         string
	EmptyTree    bool // Repository had no HEAD at startup; Base holds its tree.
	Untracked    []string
	Fingerprints map[string][32]byte
}

type Diff struct {
	Files   []FileDelta
	Added   int
	Deleted int
}

type FileDelta struct {
	Path    string
	Added   int
	Deleted int
	New     bool
}

func parseNumstat(record []byte) (FileDelta, error) {
	parts := strings.SplitN(string(record), "\t", 3)
	if len(parts) != 3 {
		return FileDelta{}, fmt.Errorf("invalid git numstat record %q", record)
	}
	f := FileDelta{Path: parts[2]}
	var err error
	if parts[0] != "-" {
		f.Added, err = strconv.Atoi(parts[0])
		if err != nil {
			return FileDelta{}, err
		}
	}
	if parts[1] != "-" {
		f.Deleted, err = strconv.Atoi(parts[1])
		if err != nil {
			return FileDelta{}, err
		}
	}
	return f, nil
}

func runGit(dir string, args ...string) ([]byte, error) {
	// Host snapshots and baselines must not execute repo-local fsmonitor code.
	// Keep normal Git hooks intact for explicit write/worktree operations.
	safeArgs := append([]string{"-c", "core.fsmonitor=false"}, args...)
	cmd := exec.Command("git", safeArgs...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func untracked(dir string) ([]string, error) {
	out, err := runGit(dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00"), nil
}

func repositoryRoot(dir string) (string, error) {
	out, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// headCommit returns an empty string for an unborn repository. Callers first
// resolve the repository root so a non-repository remains an error.
func headCommit(dir string) (string, error) {
	out, err := runGit(dir, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return "", nil
	}
	return strings.TrimSpace(string(out)), err
}

func TakeSnapshot(dir string) (Snapshot, error) {
	// Snapshot and diff cover the repository, even when Cwd is a subdirectory.
	root, err := repositoryRoot(dir)
	if err != nil {
		return Snapshot{}, err
	}
	dir = root
	head, err := headCommit(dir)
	if err != nil {
		return Snapshot{}, err
	}
	if head == "" {
		base, excluded, err := temporaryTree(dir)
		return Snapshot{Base: base, EmptyTree: true, Untracked: excluded}, err
	}
	out, err := runGit(dir, "stash", "create")
	if err != nil {
		return Snapshot{}, err
	}
	base := strings.TrimSpace(string(out))
	if base == "" {
		out, err = runGit(dir, "rev-parse", "HEAD")
		if err != nil {
			return Snapshot{}, err
		}
		base = strings.TrimSpace(string(out))
	}
	paths, err := untracked(dir)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Base: base, Untracked: paths, Fingerprints: make(map[string][32]byte)}
	for _, path := range paths {
		info, err := os.Lstat(filepath.Join(dir, path))
		if err != nil {
			return Snapshot{}, err
		}
		// Embedded repositories and special files were excluded by path before
		// fingerprints existed. Do not traverse them or block on pipes/devices.
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		data, err := fileContent(filepath.Join(dir, path))
		if err != nil {
			return Snapshot{}, err
		}
		snap.Fingerprints[path] = sha256.Sum256(data)
	}
	return snap, nil
}

// Git stores a symlink's target text rather than the referenced content.
func fileContent(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return []byte(target), err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("cannot read non-regular file %s", path)
	}
	return os.ReadFile(path)
}

func DiffSince(dir string, snap Snapshot) (Diff, error) {
	root, err := repositoryRoot(dir)
	if err != nil {
		return Diff{}, err
	}
	dir = root
	if snap.EmptyTree {
		return diffSinceTree(dir, snap)
	}
	out, err := runGit(dir, "diff", "--numstat", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", snap.Base, "--")
	if err != nil {
		return Diff{}, err
	}
	existing := make(map[string]bool, len(snap.Untracked))
	for _, path := range snap.Untracked {
		fingerprint, recorded := snap.Fingerprints[path]
		if !recorded {
			existing[path] = true
			continue
		}
		data, err := fileContent(filepath.Join(dir, path))
		if err != nil && !os.IsNotExist(err) {
			return Diff{}, err
		}
		existing[path] = err == nil && sha256.Sum256(data) == fingerprint
	}
	d := Diff{}
	for _, record := range bytes.Split(out, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		f, err := parseNumstat(record)
		if err != nil {
			return Diff{}, err
		}
		// Unchanged user files remain excluded after git add, too.
		if existing[f.Path] {
			continue
		}
		d.Files = append(d.Files, f)
	}
	paths, err := untracked(dir)
	if err != nil {
		return Diff{}, err
	}
	for _, path := range paths {
		if existing[path] {
			continue
		}
		f, err := addedFile(dir, path)
		if err != nil {
			return Diff{}, err
		}
		d.Files = append(d.Files, f)
	}
	sort.Slice(d.Files, func(i, j int) bool { return d.Files[i].Path < d.Files[j].Path })
	for _, f := range d.Files {
		d.Added += f.Added
		d.Deleted += f.Deleted
	}
	return d, nil
}

func addedFile(dir, path string) (FileDelta, error) {
	data, err := fileContent(filepath.Join(dir, path))
	if err != nil {
		return FileDelta{}, err
	}
	f := FileDelta{Path: path, New: true}
	if !bytes.ContainsRune(data, 0) {
		f.Added = bytes.Count(data, []byte{'\n'})
		if len(data) > 0 && data[len(data)-1] != '\n' {
			f.Added++
		}
	}
	return f, nil
}

// temporaryTree stages the working tree into a private copy of the user's
// index. Copying preserves tracked ignored files; git add applies ignore rules
// to other files. Preexisting embedded repositories remain excluded by path,
// just as they are in snapshots of repositories with HEAD.
func temporaryTree(dir string) (tree string, excluded []string, err error) {
	tmp, err := os.MkdirTemp("", "pactify-sub-index-*")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(tmp)
	index := filepath.Join(tmp, "index")
	indexPath, err := runGit(dir, "rev-parse", "--git-path", "index")
	if err != nil {
		return "", nil, err
	}
	source := strings.TrimSuffix(string(indexPath), "\n")
	if !filepath.IsAbs(source) {
		source = filepath.Join(dir, source)
	}
	data, err := os.ReadFile(source)
	if err == nil {
		if err := os.WriteFile(index, data, 0600); err != nil {
			return "", nil, err
		}
	} else if !os.IsNotExist(err) {
		return "", nil, err
	}
	paths, err := untracked(dir)
	if err != nil {
		return "", nil, err
	}
	args := []string{"add", "-A", "--", "."}
	for _, path := range paths {
		info, err := os.Lstat(filepath.Join(dir, path))
		if err != nil {
			return "", nil, err
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			excluded = append(excluded, path)
			args = append(args, ":(top,exclude,literal)"+strings.TrimSuffix(path, "/"))
		}
	}
	run := func(args ...string) ([]byte, error) {
		// Host snapshots and baselines must not execute repo-local fsmonitor code.
		// Keep normal Git hooks intact for explicit write/worktree operations.
		safeArgs := append([]string{"-c", "core.fsmonitor=false"}, args...)
		cmd := exec.Command("git", safeArgs...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+index)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
		}
		return out, nil
	}
	if _, err := run(args...); err != nil {
		return "", nil, err
	}
	out, err := run("write-tree")
	return strings.TrimSpace(string(out)), excluded, err
}

func diffSinceTree(dir string, snap Snapshot) (Diff, error) {
	if snap.Base == "" {
		return Diff{}, fmt.Errorf("baseline tree unavailable; changes cannot be attributed to this job")
	}
	current, excluded, err := temporaryTree(dir)
	if err != nil {
		return Diff{}, err
	}
	out, err := runGit(dir, "diff", "--numstat", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", snap.Base, current, "--")
	if err != nil {
		return Diff{}, err
	}
	added, err := runGit(dir, "diff", "--name-only", "-z", "--no-renames", "--diff-filter=A", snap.Base, current, "--")
	if err != nil {
		return Diff{}, err
	}
	newPaths := make(map[string]bool)
	for _, path := range bytes.Split(added, []byte{0}) {
		newPaths[string(path)] = true
	}
	d := Diff{}
	for _, record := range bytes.Split(out, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		f, err := parseNumstat(record)
		if err != nil {
			return Diff{}, err
		}
		f.New = newPaths[f.Path]
		d.Files = append(d.Files, f)
		d.Added += f.Added
		d.Deleted += f.Deleted
	}
	var failures []error
	for _, path := range excluded {
		if !slices.Contains(snap.Untracked, path) {
			failures = append(failures, fmt.Errorf("cannot count non-regular path %q", path))
		}
	}
	sort.Slice(d.Files, func(i, j int) bool { return d.Files[i].Path < d.Files[j].Path })
	return d, errors.Join(failures...)
}
