package sub

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const doctorDiskMinimum = uint64(5 << 30)

// This is a host check, separate from the per-harness probes and their verified
// version records. Missing storage must not create a home or a jobs directory.
func doctorStorageChecks(store Store, free func(string) (uint64, error)) []DoctorCheck {
	if free == nil {
		free = availableDiskSpace
	}
	home := filepath.Dir(store.Root)
	disk := DoctorCheck{Name: "Disk space", Status: "PASS"}
	available, err := free(home)
	if err != nil {
		disk.Status, disk.Reason = "WARN", fmt.Sprintf("%s: available space unknown: %v", home, err)
	} else {
		if available < doctorDiskMinimum {
			disk.Status = "WARN"
		}
		disk.Reason = fmt.Sprintf("%s: %.2f GiB available (%d bytes); warn below 5 GiB", home, float64(available)/(1<<30), available)
	}
	path := filepath.Join(home, "cache")
	cache := DoctorCheck{Name: "Shared cache", Status: "PASS"}
	size, err := cacheSize(path)
	if err != nil {
		cache.Status, cache.Reason = "WARN", fmt.Sprintf("%s: size unknown: %v", path, err)
	} else {
		cache.Reason = fmt.Sprintf("%s: %.2f GiB (%d bytes of files)", path, float64(size)/(1<<30), size)
	}
	checks := []DoctorCheck{disk, cache}
	var reader StorageReader
	storage, e := reader.Snapshot(context.Background(), store)
	if e != nil {
		return append(checks, DoctorCheck{Name: "Storage lifecycle", Status: "WARN", Reason: e.Error()})
	}
	for _, location := range storage.Locations {
		if location.Name == "cache" {
			continue
		}
		name := "Jobs"
		if location.Name == "harness-homes" {
			name = "Harness homes"
		}
		checks = append(checks, DoctorCheck{Name: name, Status: "PASS", Reason: fmt.Sprintf("%s: %d bytes; %d bytes reclaimable", filepath.Join(home, location.Name), location.Bytes, location.Reclaimable)})
	}
	last := DoctorCheck{Name: "Last prune", Status: "PASS", Reason: "never run"}
	if storage.LastRun != nil {
		r := storage.LastRun
		last.Reason = r.StartedAt.Format(time.RFC3339)
		if r.EndedAt.IsZero() {
			last.Status = "WARN"
			last.Reason += " (running or interrupted)"
		} else {
			last.Reason += "; finished " + r.EndedAt.Format(time.RFC3339)
		}
		if len(r.Failures) > 0 {
			last.Status = "WARN"
			last.Reason += "; " + strings.Join(r.Failures, "; ")
		}
	}
	checks = append(checks, last)
	if len(storage.StaleWorktrees) > 0 {
		checks = append(checks, DoctorCheck{Name: "Stale worktrees", Status: "WARN", Reason: strings.Join(storage.StaleWorktrees, ", ") + "; retained until explicitly applied/discarded or pactify sub prune --yes --include-worktrees"})
	}
	for _, failure := range storage.Failures {
		checks = append(checks, DoctorCheck{Name: "Storage inventory", Status: "WARN", Reason: failure})
	}
	return checks
}

func availableDiskSpace(path string) (uint64, error) {
	// PACTIFY_HOME may not exist yet. Query its nearest existing ancestor's
	// filesystem without making the diagnostic mutate the user's storage.
	for {
		var stat unix.Statfs_t
		err := unix.Statfs(path, &stat)
		if err == nil {
			return stat.Bavail * uint64(stat.Bsize), nil
		}
		if !os.IsNotExist(err) {
			return 0, err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, err
		}
		path = parent
	}
}

func cacheSize(path string) (int64, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("cache must be a real directory")
	}
	var size int64
	err = filepath.WalkDir(path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Do not follow links inside a cache into user data or another cache.
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}
