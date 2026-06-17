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
