package sub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agentjoey/pactify/internal/lockx"
)

type homeLink struct {
	Target    string
	Directory bool
}

// managedHome inspects metadata only. Paths below symlinked ancestors are never
// created or removed; RemoveAll removes leaf links without traversing them.
type managedHome struct {
	Name               string
	Home               string
	Source             string
	Links              map[string]homeLink
	AllowMissingSource bool
	Mirror             bool
	Exclude            []string
	Remove             []string
}

type harnessHome struct {
	Additional   []*harnessHome
	BeforeDoctor func() (func() error, error)
	Env          string
	Source       func() (string, error)
	Managed      func() (string, error)
	Layout       func(home, source string) *managedHome
	Doctor       func(Store, string, Result, bool) error
}

func managedHarnessHome(name string) (string, error) {
	store, err := DefaultStore()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(filepath.Dir(store.Root), "harness-homes", name))
}
func (m *managedHome) excluded(name string) bool {
	for _, s := range m.Exclude {
		if s == name {
			return true
		}
	}
	return false
}
func (m *managedHome) guard() error {
	if info, err := os.Lstat(m.Home); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s managed home must be a directory, not a symlink or file: %s", m.Name, m.Home)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	// Resolve parent aliases even before the managed directory exists.
	resolve := func(path string) (string, error) {
		path, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		var tail []string
		for {
			resolved, err := filepath.EvalSymlinks(path)
			if err == nil {
				for i := len(tail) - 1; i >= 0; i-- {
					resolved = filepath.Join(resolved, tail[i])
				}
				return resolved, nil
			}
			if !os.IsNotExist(err) {
				return "", err
			}
			parent := filepath.Dir(path)
			if parent == path {
				return "", err
			}
			tail = append(tail, filepath.Base(path))
			path = parent
		}
	}
	home, err := resolve(m.Home)
	if err != nil {
		return err
	}
	source, err := resolve(m.Source)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(home, source)
	if err != nil {
		return err
	}
	if rel == "." || filepath.IsLocal(rel) {
		return fmt.Errorf("%s user home must be outside the managed home: %s", m.Name, m.Home)
	}
	return nil
}
func (m *managedHome) path(name string) (string, error) {
	if !filepath.IsLocal(name) || name == "." {
		return "", fmt.Errorf("invalid managed relative path %q", name)
	}
	path := m.Home
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		path = filepath.Join(path, part)
		if info, err := os.Lstat(path); err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("%s managed path ancestor is not a real directory: %s", m.Name, path)
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return filepath.Join(m.Home, name), nil
}
func (m *managedHome) links() (map[string]homeLink, error) {
	links := map[string]homeLink{}
	for name, link := range m.Links {
		links[name] = link
	}
	if m.Mirror {
		entries, err := os.ReadDir(m.Source)
		if err != nil {
			if !m.AllowMissingSource || !os.IsNotExist(err) {
				return nil, err
			}
		}
		for _, entry := range entries {
			if !m.excluded(entry.Name()) {
				links[entry.Name()] = homeLink{Target: filepath.Join(m.Source, entry.Name()), Directory: entry.IsDir()}
			}
		}
	}
	return links, nil
}
func sortedHomeLinks(links map[string]homeLink) []string {
	names := make([]string, 0, len(links))
	for name := range links {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Open a stable directory handle before mutating. Root confines operations even
// if a running harness replaces an ancestor between the metadata check and use.
func (m *managedHome) openRoot() (*os.Root, error) {
	info, err := os.Lstat(m.Home)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s managed home was replaced", m.Name)
	}
	root, err := os.OpenRoot(m.Home)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("%s managed home changed while opening it", m.Name)
	}
	return root, nil
}

func (m *managedHome) Prepare(ctx context.Context) (prepareErr error) {
	release, err := lockx.Acquire(ctx, m.Home+".lock")
	if err != nil {
		return err
	}
	defer release()
	if err = m.guard(); err != nil {
		return err
	}
	defer func() {
		if prepareErr != nil {
			if warnings := m.cleanupEntries(); len(warnings) > 0 {
				prepareErr = fmt.Errorf("%w; %s", prepareErr, strings.Join(warnings, "; "))
			}
		}
	}()
	if _, err = filepath.EvalSymlinks(m.Source); err != nil && !(m.AllowMissingSource && os.IsNotExist(err)) {
		return fmt.Errorf("%s user home unavailable; run %s login first: %w", m.Name, m.Name, err)
	}
	links, err := m.links()
	if err != nil {
		return err
	}
	for _, name := range sortedHomeLinks(links) {
		if _, err = m.path(name); err != nil {
			return err
		}
		link := links[name]
		// Mirrored HOME entries may themselves be dangling links. Preserve those
		// aliases without following them; explicit login/config sources must exist.
		if m.Mirror {
			if _, explicit := m.Links[name]; !explicit {
				continue
			}
		}
		info, err := os.Stat(link.Target)
		if err != nil {
			return fmt.Errorf("%s %s unavailable; run %s login first: %w", m.Name, name, m.Name, err)
		}
		if (link.Directory && !info.IsDir()) || (!link.Directory && !info.Mode().IsRegular()) {
			return fmt.Errorf("%s %s has an unexpected type; run %s login first", m.Name, name, m.Name)
		}
	}
	if err = os.MkdirAll(m.Home, 0700); err != nil {
		return err
	}
	root, err := m.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.Chmod(".", 0700); err != nil {
		return err
	}
	if m.Mirror {
		entries, err := os.ReadDir(m.Home)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if m.excluded(name) {
				continue
			}
			if _, ok := links[name]; !ok {
				if err = root.RemoveAll(name); err != nil {
					return err
				}
			}
		}
	}
	for _, name := range sortedHomeLinks(links) {
		path, err := m.path(name)
		if err != nil {
			return err
		}
		target := links[name].Target
		if current, err := os.Readlink(path); err == nil && current == target {
			continue
		}
		if err = root.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return err
		}
		if err = root.RemoveAll(name); err != nil {
			return err
		}
		if err = root.Symlink(target, name); err != nil {
			return err
		}
	}
	for _, name := range m.Remove {
		if _, err := m.path(name); err != nil {
			return err
		}
		if err = root.RemoveAll(name); err != nil {
			return err
		}
	}
	return nil
}
func (m *managedHome) Check() error {
	if err := m.guard(); err != nil {
		return err
	}
	info, err := os.Lstat(m.Home)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return fmt.Errorf("%s managed home must be a 0700 directory", m.Name)
	}
	links, err := m.links()
	if err != nil {
		return err
	}
	for _, name := range sortedHomeLinks(links) {
		path, err := m.path(name)
		if err != nil {
			return err
		}
		target, err := os.Readlink(path)
		if err != nil {
			return fmt.Errorf("%s %s is not an intact symlink: %w", m.Name, name, err)
		}
		if target != links[name].Target {
			return fmt.Errorf("%s %s points to the wrong source", m.Name, name)
		}
		if m.Mirror {
			if _, explicit := m.Links[name]; !explicit {
				continue
			}
		}
		if _, err = os.Stat(path); err != nil {
			return fmt.Errorf("%s %s source unavailable: %w", m.Name, name, err)
		}
	}
	if m.Mirror {
		entries, err := os.ReadDir(m.Home)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !m.excluded(entry.Name()) {
				if _, ok := links[entry.Name()]; !ok {
					return fmt.Errorf("%s managed home contains a stale entry: %s", m.Name, entry.Name())
				}
			}
		}
	}
	for _, name := range m.Remove {
		path, err := m.path(name)
		if err != nil {
			return err
		}
		if _, err = os.Lstat(path); !os.IsNotExist(err) {
			return fmt.Errorf("%s managed home contains %s or cannot be inspected", m.Name, name)
		}
	}
	return nil
}
func (m *managedHome) Cleanup() []string {
	release, err := lockx.Acquire(context.Background(), m.Home+".lock")
	if err != nil {
		return []string{fmt.Sprintf("%s home link check failed: %v", m.Name, err)}
	}
	defer release()
	return m.cleanupEntries()
}
func (m *managedHome) cleanupEntries() []string {
	if _, err := os.Lstat(m.Home); os.IsNotExist(err) {
		return nil
	}
	if err := m.guard(); err != nil {
		return []string{fmt.Sprintf("%s home link check failed: %v; refusing to follow it", m.Name, err)}
	}
	root, err := m.openRoot()
	if err != nil {
		return []string{fmt.Sprintf("%s home link check failed: %v", m.Name, err)}
	}
	defer root.Close()
	names := map[string]homeLink{}
	for name := range m.Links {
		names[name] = homeLink{}
	}
	if m.Mirror {
		entries, err := os.ReadDir(m.Home)
		if err != nil {
			return []string{fmt.Sprintf("%s home link check failed: %v", m.Name, err)}
		}
		for _, entry := range entries {
			if !m.excluded(entry.Name()) {
				names[entry.Name()] = homeLink{}
			}
		}
	}
	var warnings []string
	for _, name := range sortedHomeLinks(names) {
		path, err := m.path(name)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s home link check failed for %s: %v", m.Name, name, err))
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		warning := fmt.Sprintf("%s replaced the %s symlink with a private copy; removed it to avoid retaining credentials (the link will be rebuilt on next launch)", m.Name, name)
		if _, explicit := m.Links[name]; !explicit {
			// A mirrored HOME entry: the session created or atomically rewrote a
			// top-level file in its private HOME. It is not a credential copy.
			warning = fmt.Sprintf("the %s session wrote %s in its private HOME; removed it (such changes do not reach your HOME)", m.Name, name)
		}
		if err = root.RemoveAll(name); err != nil {
			warning = fmt.Sprintf("%s: could not remove %s from the private HOME: %v", m.Name, name, err)
		}
		warnings = append(warnings, warning)
	}
	return warnings
}

// directories keeps the primary home contract used by older job records.
func (h *harnessHome) directories() []*harnessHome {
	return append([]*harnessHome{h}, h.Additional...)
}

// prepareHarnessHomes is shared by model discovery and the job lifecycle.
// Saved paths win on resume, even when the caller's XDG defaults have changed.
func prepareHarnessHomes(ctx context.Context, h *harnessHome, primary string, saved map[string]string, minimal bool) (string, map[string]string, []string, []*managedHome, error) {
	homes := map[string]string{}
	var env []string
	var layouts []*managedHome
	for i, d := range h.directories() {
		source, err := d.Source()
		if err != nil {
			return primary, homes, env, layouts, err
		}
		source, err = filepath.Abs(source)
		if err != nil {
			return primary, homes, env, layouts, err
		}
		home := saved[d.Env]
		if i == 0 {
			home = primary
		}
		if home == "" {
			home = source
			if minimal {
				home, err = d.Managed()
				if err != nil {
					return primary, homes, env, layouts, err
				}
			}
		}
		if i == 0 {
			primary = home
		} else {
			homes[d.Env] = home
		}
		if minimal {
			layout := d.Layout(home, source)
			layouts = append(layouts, layout)
			if err = layout.Prepare(ctx); err != nil {
				return primary, homes, env, layouts, err
			}
		}
		env = append(env, d.Env+"="+home)
	}
	return primary, homes, env, layouts, nil
}
