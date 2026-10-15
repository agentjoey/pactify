package sub

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Snapshot struct {
	Base      string
	Untracked []string
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

func runGit(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
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

func TakeSnapshot(dir string) (Snapshot, error) {
	// Snapshot and diff cover the repository, even when Cwd is a subdirectory.
	root, err := repositoryRoot(dir)
	if err != nil {
		return Snapshot{}, err
	}
	dir = root
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
	return Snapshot{Base: base, Untracked: paths}, nil
}

func DiffSince(dir string, snap Snapshot) (Diff, error) {
	root, err := repositoryRoot(dir)
	if err != nil {
		return Diff{}, err
	}
	dir = root
	out, err := runGit(dir, "diff", "--numstat", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", snap.Base, "--")
	if err != nil {
		return Diff{}, err
	}
	existing := make(map[string]bool, len(snap.Untracked))
	for _, path := range snap.Untracked {
		existing[path] = true
	}
	d := Diff{}
	for _, record := range bytes.Split(out, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		parts := strings.SplitN(string(record), "\t", 3)
		if len(parts) != 3 {
			return Diff{}, fmt.Errorf("invalid git numstat record %q", record)
		}
		f := FileDelta{Path: parts[2]}
		// Pre-existing untracked files remain user-owned after git add, too.
		if existing[f.Path] {
			continue
		}
		if parts[0] != "-" {
			f.Added, err = strconv.Atoi(parts[0])
			if err != nil {
				return Diff{}, err
			}
		}
		if parts[1] != "-" {
			f.Deleted, err = strconv.Atoi(parts[1])
			if err != nil {
				return Diff{}, err
			}
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
		fullPath := filepath.Join(dir, path)
		info, err := os.Lstat(fullPath)
		if err != nil {
			return Diff{}, err
		}
		var data []byte
		if info.Mode()&os.ModeSymlink != 0 {
			// Git stores the link target text, not the referenced file's contents.
			target, linkErr := os.Readlink(fullPath)
			if linkErr != nil {
				return Diff{}, linkErr
			}
			data = []byte(target)
		} else {
			data, err = os.ReadFile(fullPath)
			if err != nil {
				return Diff{}, err
			}
		}
		f := FileDelta{Path: path, New: true}
		if !bytes.ContainsRune(data, 0) {
			f.Added = bytes.Count(data, []byte{'\n'})
			if len(data) > 0 && data[len(data)-1] != '\n' {
				f.Added++
			}
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
