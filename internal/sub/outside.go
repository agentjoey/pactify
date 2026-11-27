package sub

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Harnesses without an OS sandbox can write anywhere the user can. Pactify cannot
// stop that, but it can notice changes to the places where a stray or malicious
// write does lasting harm: git control files run by the user's next git command,
// and shell, git, ssh and launch agent configuration in the user's home.

// OutsideSnapshot fingerprints those locations when a job starts.
type OutsideSnapshot struct {
	Entries map[string]string `json:"entries"` // absolute path -> fingerprint
}

// OutsideChange is one watched path that differed when the job finished.
type OutsideChange struct {
	Path   string `json:"path"`
	Change string `json:"change"` // added, modified or removed
}

const (
	outsideEntryLimit = 2000
	outsideHashLimit  = 8 << 20
)

var outsideHomePaths = []string{
	".zshrc", ".zshenv", ".zprofile", ".zlogin", ".bashrc", ".bash_profile", ".profile",
	".gitconfig", ".config/git", ".ssh", "Library/LaunchAgents",
}

var outsideGitPaths = []string{"hooks", "config", "info"}

// osSandboxed reports whether the harness confines writes with its own OS sandbox.
func osSandboxed(harness string) bool {
	return harness == "codex" || harness == "dsh"
}

func requiresOutsideBaseline(req Request) bool {
	return !osSandboxed(req.Harness)
}

func outsideRoots(cwd string) []string {
	var roots []string
	if out, err := runGit(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
		common := strings.TrimSpace(string(out))
		for _, p := range outsideGitPaths {
			roots = append(roots, filepath.Join(common, p))
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, p := range outsideHomePaths {
			roots = append(roots, filepath.Join(home, p))
		}
	}
	return roots
}

// captureOutside never follows symbolic links: a link is recorded by its target.
func captureOutside(cwd string) (*OutsideSnapshot, error) {
	snap := &OutsideSnapshot{Entries: map[string]string{}}
	for _, root := range outsideRoots(cwd) {
		count := 0
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					snap.Entries[path] = "unreadable"
				}
				return nil
			}
			if count >= outsideEntryLimit {
				return filepath.SkipAll
			}
			if path != root && strings.HasPrefix(entry.Name(), "known_hosts") {
				return nil // Updated by ordinary ssh use.
			}
			count++
			print, err := outsideFingerprint(path, entry)
			if err != nil {
				return err
			}
			snap.Entries[path] = print
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", root, err)
		}
	}
	return snap, nil
}

func outsideFingerprint(path string, entry fs.DirEntry) (string, error) {
	info, err := entry.Info()
	if err != nil {
		return "", err
	}
	mode := info.Mode()
	switch {
	case mode&fs.ModeSymlink != 0:
		target, err := os.Readlink(path)
		return "link:" + target, err
	case mode.IsDir():
		return fmt.Sprintf("dir:%o", mode.Perm()), nil
	case !mode.IsRegular():
		return fmt.Sprintf("other:%s", mode), nil
	case info.Size() > outsideHashLimit:
		return fmt.Sprintf("file:%o:%d:%d", mode.Perm(), info.Size(), info.ModTime().UnixNano()), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("unreadable:%o:%d:%d", mode.Perm(), info.Size(), info.ModTime().UnixNano()), nil
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("file:%o:%s", mode.Perm(), hex.EncodeToString(h.Sum(nil))), nil
}

func compareOutside(before, after *OutsideSnapshot) []OutsideChange {
	var changes []OutsideChange
	for path, print := range after.Entries {
		if old, ok := before.Entries[path]; !ok {
			changes = append(changes, OutsideChange{path, "added"})
		} else if old != print {
			changes = append(changes, OutsideChange{path, "modified"})
		}
	}
	for path := range before.Entries {
		if _, ok := after.Entries[path]; !ok {
			changes = append(changes, OutsideChange{path, "removed"})
		}
	}
	sortChanges(changes)
	return changes
}

func sortChanges(changes []OutsideChange) {
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
}

const outsideUncheckedWarning = "Files outside the workspace were not checked: no baseline was recorded when the job started"

const outsideChangesPrefix = "Files outside the workspace changed while the job ran"

func outsideChangesWarning(changes []OutsideChange) string {
	var paths []string
	for i, c := range changes {
		if i == 5 {
			paths = append(paths, fmt.Sprintf("and %d more", len(changes)-5))
			break
		}
		paths = append(paths, c.Change+" "+c.Path)
	}
	return fmt.Sprintf("%s (this harness has no OS sandbox; another process may also be responsible): %s", outsideChangesPrefix, strings.Join(paths, ", "))
}

// checkOutside records changes since the job's baseline in the result.
func checkOutside(st *State, result *Result) {
	if st.OutsideBaseline == nil {
		if requiresOutsideBaseline(st.Request) && !st.StartedAt.IsZero() {
			result.Warnings = append(result.Warnings, outsideUncheckedWarning)
		}
		return
	}
	current, err := captureOutside(st.Request.Cwd)
	if err != nil {
		result.Warnings = append(result.Warnings, "Failed to check files outside the workspace: "+err.Error())
		return
	}
	result.OutsideChanges = compareOutside(st.OutsideBaseline, current)
	if len(result.OutsideChanges) > 0 {
		result.Warnings = append(result.Warnings, outsideChangesWarning(result.OutsideChanges))
	}
}
