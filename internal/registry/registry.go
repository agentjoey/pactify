// Package registry persists the set of .pact/ projects pactify serve watches.
package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
func EnsureRegistered(path string) (name string, added bool, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
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
