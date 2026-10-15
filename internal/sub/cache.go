package sub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/cockpit"
)

// Cache variables are process-local. Explicit paths can be granted individually
// to codex rather than granting the entire Pactify home or the user's home.
func prepareJobCaches(home string, codex bool, opts *cockpit.StartOpts) ([]string, error) {
	home, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	var warnings []string
	for _, entry := range []struct{ key, dir string }{
		{"GOCACHE", "go-build"}, {"GOMODCACHE", "go-mod"}, {"npm_config_cache", "npm"},
	} {
		value := os.Getenv(entry.key)
		if entry.key == "npm_config_cache" && value == "" {
			value = os.Getenv("NPM_CONFIG_CACHE")
		}
		// Go reserves "off" to disable its build cache; it is not a path.
		if entry.key == "GOCACHE" && value == "off" {
			opts.Env = append(opts.Env, entry.key+"="+value)
			continue
		}
		path := value
		if path != "" && !filepath.IsAbs(path) {
			path = filepath.Join(opts.RepoDir, path)
		}
		fallback := false
		if value != "" && codex && opts.Access == cockpit.AccessWrite {
			fallback = cacheDirectoryWritable(path) != nil
		}
		if value == "" || fallback {
			path = filepath.Join(home, "cache", entry.dir)
			if err := os.MkdirAll(path, 0700); err != nil {
				return warnings, fmt.Errorf("prepare %s: %w", entry.key, err)
			}
			value = path
			if fallback {
				warnings = append(warnings, fmt.Sprintf("%s is not writable; using shared cache %s", entry.key, path))
			}
		}
		opts.Env = append(opts.Env, entry.key+"="+value)
		if codex && opts.Access == cockpit.AccessWrite {
			opts.WritableRoots = append(opts.WritableRoots, path)
		}
	}
	return warnings, nil
}

// Probe only the cache root, never chmod module-cache contents. Downloaded Go
// modules keep their normal read-only permissions and are safe to share.
func cacheDirectoryWritable(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(path, ".pactify-write-*")
	if err != nil {
		return err
	}
	name := f.Name()
	closeErr := f.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

// Reject symlinked job directories, including links to other jobs within the
// store. Root keeps recursive temp cleanup confined even if inner paths change.
func (s Store) openJobRoot(id string) (*os.Root, error) {
	if err := validJobID(id); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(id)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("job %s must be a real directory", id)
	}
	job, err := root.OpenRoot(id)
	if err != nil {
		return nil, err
	}
	opened, err := job.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		job.Close()
		return nil, fmt.Errorf("job %s directory changed", id)
	}
	return job, nil
}

func (s Store) prepareJobTemp(ctx context.Context, id string) error {
	job, err := s.openJobRoot(id)
	if err != nil {
		return err
	}
	defer job.Close()
	return s.WithState(ctx, id, func(st *State) error {
		// Do not adopt or remove a preexisting tmp directory with unknown ownership.
		if err := job.Mkdir("tmp", 0700); err != nil {
			return fmt.Errorf("create job temp: %w", err)
		}
		st.TempDirOwned = true
		if err := s.Save(*st); err != nil {
			_ = job.RemoveAll("tmp")
			return err
		}
		return nil
	})
}

func (s Store) cleanupJobTemp(id string) error {
	job, err := s.openJobRoot(id)
	if err != nil {
		return err
	}
	defer job.Close()
	st, err := s.Load(id)
	if err != nil {
		return err
	}
	if !st.TempDirOwned {
		return nil
	}
	return job.RemoveAll("tmp")
}
