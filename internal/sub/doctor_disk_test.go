package sub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorDiskSpaceAndCacheSize(t *testing.T) {
	const gb = uint64(1 << 30)
	for _, tc := range []struct {
		name      string
		available uint64
		err       error
		status    string
	}{
		{"low", 5*gb - 1, nil, "WARN"},
		{"boundary", 5 * gb, nil, "PASS"},
		{"plenty", 20 * gb, nil, "PASS"},
		{"unknown", 0, errors.New("statfs failed"), "WARN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			cache := filepath.Join(home, "cache", "go-build")
			if err := os.MkdirAll(cache, 0700); err != nil {
				t.Fatal(err)
			}
			write(t, cache, "object", "1234567")
			outside := t.TempDir()
			write(t, outside, "user-file", "must not count")
			if err := os.Symlink(outside, filepath.Join(cache, "link")); err != nil {
				t.Fatal(err)
			}
			checks := doctorStorageChecks(Store{Root: filepath.Join(home, "jobs")}, func(path string) (uint64, error) {
				if path != home {
					t.Errorf("checking wrong volume: %s", path)
				}
				return tc.available, tc.err
			})
			if len(checks) != 2 || checks[0].Name != "Disk space" || checks[0].Status != tc.status || checks[1].Name != "Shared cache" || checks[1].Status != "PASS" || !strings.Contains(checks[1].Reason, "7 bytes") || !strings.Contains(checks[1].Reason, filepath.Join(home, "cache")) {
				t.Fatalf("checks=%+v", checks)
			}
			if !(DoctorReport{Checks: checks}).Passed() {
				t.Fatal("storage warnings must not fail doctor")
			}
		})
	}
}

func TestDoctorPrintsStorageChecksWithoutCreatingJobs(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	store, _ := DefaultStore()
	var output strings.Builder
	_, _ = Doctor(context.Background(), store, []string{"codex"}, &output)
	if !strings.Contains(output.String(), "Disk space:") || !strings.Contains(output.String(), "Shared cache:") {
		t.Fatal(output.String())
	}
	if _, err := os.Stat(store.Root); !os.IsNotExist(err) {
		t.Fatalf("doctor created jobs: %v", err)
	}
}

func TestDoctorMissingHomeUsesExistingVolume(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-created", "home")
	checks := doctorStorageChecks(Store{Root: filepath.Join(home, "jobs")}, nil)
	if len(checks) != 2 || checks[0].Status != "PASS" && checks[0].Status != "WARN" || checks[1].Status != "PASS" || !strings.Contains(checks[1].Reason, "0 bytes") {
		t.Fatalf("checks=%+v", checks)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("created home: %v", err)
	}
}

func TestDoctorUsesCallerSharedCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	h, _ := Lookup("codex")
	h.Version = func(context.Context) (string, error) { return "test", nil }
	calls := 0
	_, err := runDoctor(context.Background(), Store{Root: filepath.Join(home, "jobs")}, []Harness{h}, doctorDeps{
		lookPath: func(string) (string, error) { return "codex", nil },
		run: func(_ context.Context, store Store, _ string, _ Harness, cfg WorkerConfig) (Result, error) {
			calls++
			if cfg.CacheHome != home || filepath.Dir(store.Root) == home {
				t.Errorf("cache not shared independently of disposable jobs: %+v %s", cfg, store.Root)
			}
			return Result{Status: StatusFailed}, nil
		},
	})
	if err != nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
