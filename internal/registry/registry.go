// Package registry persists the set of .pact/ projects pactify serve watches.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type Project struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Group string `json:"group,omitempty"`
}

type Registry struct {
	Projects []Project `json:"projects"`
}

func file() string {
	if h := os.Getenv("PACTIFY_HOME"); h != "" {
		return filepath.Join(h, "projects.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pactify", "projects.json")
}

// Path is the absolute path of the registry file (honoring PACTIFY_HOME). Serve
// watches it so a CLI `register`/auto-register becomes live without a restart.
func Path() string { return file() }

var slugRe = regexp.MustCompile(`[^a-z0-9-]+`)

// Slug lowercases and replaces runs of non-slug chars with a single dash.
func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// Load reads the registry; a missing file is an empty registry.
func Load() (Registry, error) {
	var r Registry
	b, err := os.ReadFile(file())
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, err
	}
	return r, nil
}

// Save writes the registry (creating the parent dir).
func (r Registry) Save() error {
	if err := os.MkdirAll(filepath.Dir(file()), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file(), b, 0o644)
}

// Add registers a project. An empty name defaults to the slug of the path's basename.
func (r *Registry) Add(name, path, group string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	name = Slug(name)
	if name == "" {
		return fmt.Errorf("registry: could not derive a name; pass --name")
	}
	for _, p := range r.Projects {
		if p.Name == name {
			return fmt.Errorf("registry: name %q already registered", name)
		}
	}
	r.Projects = append(r.Projects, Project{Name: name, Path: abs, Group: group})
	return nil
}

// Missing reports whether a registered project no longer resolves to a real pact
// project on disk — its directory is gone, or the directory survives but its
// `.pact/` no longer does.
//
// This exists because a dead registration is otherwise INDISTINGUISHABLE from a
// healthy but empty one: both surface as zero features, so the dashboard renders
// a normal-looking board with nothing in it and the user reads that as "the tool
// is broken" (reported 2026-08-22 against a project auto-registered at a since-
// deleted $TMPDIR path).
//
// It is deliberately a FLAG, never an auto-delete: a path can be missing because
// a volume is unmounted or a worktree is temporarily away, and silently dropping
// the user's registration for that would be worse than showing it as unavailable.
// `pactify unregister <name>` remains the way to remove one.
func Missing(path string) bool {
	if path == "" {
		return true
	}
	fi, err := os.Stat(filepath.Join(path, ".pact"))
	return err != nil || !fi.IsDir()
}

// ErrTempPath is what EnsureRegistered returns instead of registering a path
// that lives under a system temp root. Callers match it with errors.Is when they
// want to distinguish "deliberately skipped" from "registration actually broke";
// autoRegister does not need to — both are a non-fatal note.
var ErrTempPath = errors.New("path is under the system temp dir")

// allowTempEnv opts OUT of the temp guard for one process. It exists for test
// harnesses (the Go suite and the bats e2e both run whole projects out of
// t.TempDir()/BATS_TEST_TMPDIR, where auto-registration is the behavior under
// test) — NOT as a user-facing feature. It is deliberately not keyed on
// PACTIFY_HOME: PACTIFY_HOME is a legitimate production override, and a user who
// relocates their registry must not silently lose this protection.
const allowTempEnv = "PACTIFY_ALLOW_TEMP_REGISTER"

func allowTempRegister() bool {
	v := os.Getenv(allowTempEnv)
	return v != "" && v != "0" && v != "false"
}

// tempRoots returns the directory prefixes treated as throwaway scratch space.
//
// Each root is recorded in BOTH its raw and its symlink-resolved spelling,
// because macOS names the same directory two ways: $TMPDIR reads as
// /var/folders/... while the real path is /private/var/folders/..., and /tmp is a
// symlink to /private/tmp. Comparing only one spelling is precisely the
// /tmp-vs-/private/tmp mismatch that has already defeated a fix in this repo, so
// the normalization is done on both sides — here, and on the candidate path in
// TempPath.
//
// /tmp is added explicitly on top of os.TempDir(): on macOS TMPDIR points at
// per-user /var/folders storage, so /tmp would otherwise not be covered at all,
// yet `mktemp -d /tmp/...` and hand-made /tmp/scratch dirs are exactly the
// throwaway projects this guard is for.
func tempRoots() []string {
	var roots []string
	add := func(p string) {
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return
		}
		roots = append(roots, filepath.Clean(abs))
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			roots = append(roots, filepath.Clean(real))
		}
	}
	add(os.TempDir())
	if runtime.GOOS != "windows" {
		add("/tmp")
	}
	return roots
}

// TempPath reports whether path lives under a system temp root (or is one).
//
// Non-existent paths are handled: EvalSymlinks fails on them, so only the literal
// spelling is compared — which still matches, because tempRoots carries both
// spellings of every root.
func TempPath(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	spellings := []string{filepath.Clean(abs)}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		spellings = append(spellings, filepath.Clean(real))
	}
	for _, root := range tempRoots() {
		for _, s := range spellings {
			if s == root || strings.HasPrefix(s, root+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

// Remove deletes a project by name.
func (r *Registry) Remove(name string) error {
	name = Slug(name)
	for i, p := range r.Projects {
		if p.Name == name {
			r.Projects = append(r.Projects[:i], r.Projects[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("registry: name %q not found", name)
}

// Rename changes a project's registry name, preserving its path and group. The
// new name is slugged (same rule as Add). Errors if oldName is unknown or the
// slugged newName is empty or already taken by a different project.
func (r *Registry) Rename(oldName, newName string) error {
	oldName = Slug(oldName)
	newName = Slug(newName)
	if newName == "" {
		return fmt.Errorf("registry: new name is empty after slugging")
	}
	idx := -1
	for i, p := range r.Projects {
		if p.Name == oldName {
			idx = i
		} else if p.Name == newName {
			return fmt.Errorf("registry: name %q already registered", newName)
		}
	}
	if idx == -1 {
		return fmt.Errorf("registry: name %q not found", oldName)
	}
	// Renaming to the same slug is a documented no-op (the newName-collision
	// branch above is skipped for the matched project), so this just rewrites
	// the identical name and returns nil.
	r.Projects[idx].Name = newName
	return nil
}

// EnsureRegistered idempotently registers path for serve to watch (spec:
// auto-register so an agent-started project is visible on the dashboard without
// a manual `pactify register`). If the ABSOLUTE path is already registered under
// any name, it is a no-op returning the existing name and added=false. A brand-
// new path is registered under its basename slug (added=true). A different path
// whose basename collides with an existing name returns the Add error — the
// caller (init/orchestrate) warns rather than failing, since registration is a
// convenience, not a prerequisite for the protocol.
//
// A path under a system temp root is REFUSED with ErrTempPath. Auto-registration
// is permanent while a $TMPDIR project is not, so every throwaway `mktemp -d`
// experiment used to leave a dead entry in the user's real
// ~/.pactify/projects.json — one ("agyproj") did, and selecting it on the
// dashboard produced a blank board that read as a broken product. Missing() flags
// such an entry after the fact; this stops it being created. The guard is scoped
// to the AUTOMATIC path only: `pactify register <path>` calls Add directly and
// still accepts a temp path, because typing the command is a deliberate act,
// whereas auto-registration is a side effect of `init`/`orchestrate` that the
// user never asked for.
func EnsureRegistered(path string) (name string, added bool, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	if TempPath(abs) && !allowTempRegister() {
		return "", false, fmt.Errorf("registry: %w: %s", ErrTempPath, abs)
	}
	r, err := Load()
	if err != nil {
		return "", false, err
	}
	for _, p := range r.Projects {
		if p.Path == abs {
			return p.Name, false, nil
		}
	}
	if err := r.Add("", abs, ""); err != nil {
		return "", false, err
	}
	if err := r.Save(); err != nil {
		return "", false, err
	}
	for _, p := range r.Projects {
		if p.Path == abs {
			return p.Name, true, nil
		}
	}
	return "", true, nil
}
