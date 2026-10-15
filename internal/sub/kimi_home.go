package sub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/lockx"
)

var kimiLinkedNames = []string{"config.toml", "credentials", "device_id"}

func managedKimiHome() (string, error) {
	store, err := DefaultStore()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(filepath.Dir(store.Root), "harness-homes", "kimi"))
}

// Only metadata is inspected. Configuration and credential contents are never
// opened, even when kimi has replaced a link with a private copy.
func prepareKimiHome(ctx context.Context, home, source string) (prepareErr error) {
	release, err := lockx.Acquire(ctx, home+".lock")
	if err != nil {
		return err
	}
	defer release()
	source, err = filepath.Abs(source)
	if err != nil {
		return err
	}
	// Resolve aliases before allowing removal of anything in the managed home.
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		unavailable := fmt.Errorf("kimi user home unavailable; run kimi login first: %w", err)
		// A missing source cannot alias an existing managed directory. Remove
		// old materialized copies even when login data has been removed.
		if os.IsNotExist(err) {
			if warnings := cleanupKimiHomeEntries(home); len(warnings) > 0 {
				return fmt.Errorf("%w; %s", unavailable, strings.Join(warnings, "; "))
			}
		}
		return unavailable
	}
	if info, err := os.Lstat(home); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("kimi managed home must be a directory, not a symlink or file: %s", home)
		}
		resolvedHome, err := filepath.EvalSymlinks(home)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(resolvedHome, resolvedSource)
		if err != nil {
			return err
		}
		if rel == "." || filepath.IsLocal(rel) {
			return fmt.Errorf("kimi user home must be outside the managed home: %s", home)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	// A failed startup still must not leave previously materialized credentials.
	// This runs under the same lock, after proving the directory is safe.
	defer func() {
		if prepareErr != nil {
			if warnings := cleanupKimiHomeEntries(home); len(warnings) > 0 {
				prepareErr = fmt.Errorf("%w; %s", prepareErr, strings.Join(warnings, "; "))
			}
		}
	}()
	// Validate all sources before changing any links; Stat follows source aliases
	// but reads only metadata and catches dangling source links.
	for _, name := range kimiLinkedNames {
		info, err := os.Stat(filepath.Join(source, name))
		if err != nil {
			return fmt.Errorf("kimi %s unavailable; run kimi login first: %w", name, err)
		}
		if (name == "credentials" && !info.IsDir()) || (name != "credentials" && !info.Mode().IsRegular()) {
			return fmt.Errorf("kimi %s has an unexpected type; run kimi login first", name)
		}
	}
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	if err := os.Chmod(home, 0700); err != nil {
		return err
	}
	for _, name := range kimiLinkedNames {
		path, target := filepath.Join(home, name), filepath.Join(source, name)
		if current, err := os.Readlink(path); err == nil && current == target {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		if err := os.Symlink(target, path); err != nil {
			return err
		}
	}
	// A leftover user MCP configuration would defeat isolation. Remove the entry
	// without following a symlink or reading its contents.
	return os.RemoveAll(filepath.Join(home, "mcp.json"))
}

func checkKimiLinks(home, source string) error {
	info, err := os.Lstat(home)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return fmt.Errorf("kimi managed home must be a 0700 directory")
	}
	for _, name := range kimiLinkedNames {
		path := filepath.Join(home, name)
		target, err := os.Readlink(path)
		if err != nil {
			return fmt.Errorf("kimi %s is not an intact symlink: %w", name, err)
		}
		if target != filepath.Join(source, name) {
			return fmt.Errorf("kimi %s points to the wrong source", name)
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("kimi %s source unavailable: %w", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, "mcp.json")); !os.IsNotExist(err) {
		return fmt.Errorf("kimi managed home contains mcp.json or cannot be inspected")
	}
	return nil
}

func cleanupKimiHome(home string) []string {
	release, err := lockx.Acquire(context.Background(), home+".lock")
	if err != nil {
		return []string{fmt.Sprintf("Kimi home link check failed: %v", err)}
	}
	defer release()
	return cleanupKimiHomeEntries(home)
}

func cleanupKimiHomeEntries(home string) []string {
	info, err := os.Lstat(home)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return []string{fmt.Sprintf("Kimi home link check failed: %v", err)}
	}
	if !info.IsDir() {
		return []string{"Kimi home link check failed: managed home was replaced; refusing to follow it"}
	}
	var warnings []string
	for _, name := range kimiLinkedNames {
		path := filepath.Join(home, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Kimi home link check failed for %s: %v", name, err))
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		warning := fmt.Sprintf("Kimi replaced the %s symlink with a private copy; removed it to avoid retaining credentials (the link will be rebuilt on next launch)", name)
		if err := os.RemoveAll(path); err != nil {
			warning = fmt.Sprintf("Kimi replaced the %s symlink with a private copy; removal failed: %v", name, err)
		}
		warnings = append(warnings, warning)
	}
	return warnings
}
