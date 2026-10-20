// Package credentiallinks preserves atomically refreshed login files using only
// metadata and rename. It never opens credential contents.
package credentiallinks

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type Stamp struct {
	Path          string
	Device, Inode uint64
	Size, Mtime   int64
	Mode          os.FileMode
}

func Inspect(path string) (Stamp, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Stamp{}, err
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return Stamp{}, err
	}
	if !info.Mode().IsRegular() {
		return Stamp{}, fmt.Errorf("credential source is not a regular file: %s", path)
	}
	st := info.Sys().(*syscall.Stat_t)
	return Stamp{resolved, uint64(st.Dev), st.Ino, info.Size(), info.ModTime().UnixNano(), info.Mode()}, nil
}

// QuarantineDir follows the persistent Pactify store, never a shared temp root.
// Callers with a nondefault store or disposable HOME must pass an explicit path.
func QuarantineDir() (string, error) {
	home := os.Getenv("PACTIFY_HOME")
	if home == "" {
		user, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(user, ".pactify")
	}
	return filepath.Join(home, "credential-quarantine"), nil
}

func Quarantine(path, directory string) (string, error) {
	if directory == "" {
		var err error
		directory, err = QuarantineDir()
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(directory), 0700); err != nil {
		return "", err
	}
	if err := os.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return "", fmt.Errorf("credential quarantine must be an owned 0700 directory: %s", directory)
	}
	slot, err := os.MkdirTemp(directory, "refresh-")
	if err != nil {
		return "", err
	}
	target := filepath.Join(slot, "credential")
	if err := os.Rename(path, target); err != nil {
		return "", err
	}
	return target, nil
}

// Restore returns a private atomic refresh to a source whose metadata matches.
// Detected source updates and missing baselines are quarantined. Interactive
// writes in the metadata-check/rename window are not excluded (see below).
func Restore(path, source string, before Stamp, quarantine string) (string, error) {
	return restore(path, source, before, quarantine, os.Chmod)
}

func restore(path, source string, before Stamp, quarantine string, chmod func(string, os.FileMode) error) (string, error) {
	now, err := Inspect(source)
	if err == nil && before.Path != "" && now == before {
		if err := chmod(path, before.Mode.Perm()); err != nil {
			return Quarantine(path, quarantine)
		}
		// Metadata validation and rename cannot exclude an interactive in-place
		// agy refresh in this window; managed-home leases cover pactify jobs only.
		// A concurrent in-place refresh can still occur between metadata validation and rename.
		if err := os.Rename(path, before.Path); err != nil {
			return "", err
		}
		return "", nil
	}
	return Quarantine(path, quarantine)
}

func Retained(path string, cause error) error {
	return fmt.Errorf("credential refresh retained at %s: %w; stop jobs and interactive harness sessions, then recover this file with a same-volume rename to its source or an owned 0700 quarantine and retry; do not delete it", path, cause)
}

// VerifyNoFallback protects plaintext host fallbacks created inside a fixture.
// It is harmless when absent; a present file must fail the scenario and survive.
func VerifyNoFallback(path, quarantine string) error {
	return quarantineUnexpected(path, quarantine, "plaintext host credential fallback")
}

// VerifyFixtureQuarantine moves an inner store's quarantine out of a disposable
// fixture before that fixture can be removed, without traversing its contents.
func VerifyFixtureQuarantine(path, quarantine string) error {
	return quarantineUnexpected(path, quarantine, "fixture credential quarantine")
}

func quarantineUnexpected(path, quarantine, description string) error {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return Retained(path, err)
	}
	location, err := Quarantine(path, quarantine)
	if err != nil {
		return Retained(path, err)
	}
	return fmt.Errorf("unexpected %s preserved at %s; preserve it for login recovery", description, location)
}

// VerifyFileLink is the stricter fixture contract: an atomic replacement fails
// the test and survives outside its temporary root, without changing the source.
func VerifyFileLink(path, target, quarantine string) error {
	current, err := os.Readlink(path)
	if err == nil {
		if current == target {
			return nil
		}
		return fmt.Errorf("credential link points to the wrong target: %s", path)
	}
	if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink == 0 {
		location, moveErr := Quarantine(path, quarantine)
		if moveErr != nil {
			return fmt.Errorf("CREDENTIAL REFRESH AT RISK: %w", Retained(path, moveErr))
		}
		return fmt.Errorf("CREDENTIAL LINK REPLACED: refreshed private file quarantined at %s; preserve it for login recovery", location)
	}
	return fmt.Errorf("credential link missing or unreadable: %s", path)
}
