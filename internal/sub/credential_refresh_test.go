package sub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// All files in these tests are synthetic; no user credential is opened.
func TestManagedCredentialAtomicRefresh(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(map[bool]string{false: "writeback", true: "conflict"}[concurrent], func(t *testing.T) {
			source, home := t.TempDir(), filepath.Join(t.TempDir(), "managed")
			target := filepath.Join(source, "auth.json")
			if err := os.WriteFile(target, []byte("fake-old"), 0600); err != nil {
				t.Fatal(err)
			}
			m := &managedHome{QuarantineDir: filepath.Join(t.TempDir(), "quarantine"), Name: "fake", Home: home, Source: source, Links: map[string]homeLink{"auth.json": {Target: target, Credential: true}}}
			if err := m.Prepare(context.Background()); err != nil {
				t.Fatal(err)
			}
			private := filepath.Join(home, "auth.json")
			tmp := filepath.Join(home, "refresh.tmp")
			if err := os.WriteFile(tmp, []byte("fake-refreshed"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(tmp, private); err != nil {
				t.Fatal(err)
			}
			if concurrent {
				if err := os.WriteFile(target+".tmp", []byte("fake-other-refresh"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(target+".tmp", target); err != nil {
					t.Fatal(err)
				}
			}
			warnings := m.Cleanup()
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			want := "fake-refreshed"
			if concurrent {
				want = "fake-other-refresh"
			}
			if string(data) != want {
				t.Errorf("source=%q want %q; warnings=%v", data, want, warnings)
			}
			if concurrent && !strings.Contains(strings.Join(warnings, ";"), "quarantin") {
				t.Errorf("conflict not preserved: %v", warnings)
			}
			if concurrent {
				paths, err := filepath.Glob(filepath.Join(m.QuarantineDir, "*", "credential"))
				if err != nil || len(paths) != 1 {
					t.Fatalf("quarantine files=%v err=%v", paths, err)
				}
				data, err := os.ReadFile(paths[0])
				if err != nil || string(data) != "fake-refreshed" {
					t.Fatal("conflicting synthetic refresh lost")
				}
			}
		})
	}
}

func TestManagedRefreshAcrossSharedLeases(t *testing.T) {
	source, home := t.TempDir(), filepath.Join(t.TempDir(), "managed")
	target := filepath.Join(source, "auth.json")
	if err := os.WriteFile(target, []byte("fake-old"), 0600); err != nil {
		t.Fatal(err)
	}
	newLayout := func() *managedHome {
		return &managedHome{Name: "fake", Home: home, Source: source, QuarantineDir: filepath.Join(source, "quarantine"), Links: map[string]homeLink{"auth.json": {Target: target, Credential: true}}}
	}
	first, second := newLayout(), newLayout()
	if err := first.prepare(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(home, "auth.json")
	if err := os.WriteFile(private+".tmp", []byte("fake-private-refresh"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(private+".tmp", private); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+".tmp", []byte("fake-external-refresh"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(target+".tmp", target); err != nil {
		t.Fatal(err)
	}
	if err := second.prepare(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if warnings := first.Cleanup(); len(warnings) != 0 {
		t.Fatalf("cleanup acted while another user retained the home: %v", warnings)
	}
	if _, err := os.Lstat(private); err != nil {
		t.Fatal("active user's refresh removed")
	}
	warnings := second.Cleanup()
	if !strings.Contains(strings.Join(warnings, ";"), "quarantined") {
		t.Fatalf("shared prepare overwrote source baseline: %v", warnings)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "fake-external-refresh" {
		t.Fatal("concurrent source overwritten")
	}
}

func TestManagedPrepareRecoversUncleanRefresh(t *testing.T) {
	source, home := t.TempDir(), filepath.Join(t.TempDir(), "managed")
	target := filepath.Join(source, "auth.json")
	if err := os.WriteFile(target, []byte("fake-old"), 0600); err != nil {
		t.Fatal(err)
	}
	layout := func() *managedHome {
		return &managedHome{Name: "fake", Home: home, Source: source, Links: map[string]homeLink{"auth.json": {Target: target, Credential: true}}}
	}
	if err := layout().Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(home, "auth.json")
	if err := os.WriteFile(private+".tmp", []byte("fake-refreshed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(private+".tmp", private); err != nil {
		t.Fatal(err)
	}
	// A fresh layout/process must use the persisted metadata, not delete the file.
	if err := layout().Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "fake-refreshed" {
		t.Fatal("unclean refresh discarded")
	}
	if err := layout().Check(); err != nil {
		t.Fatal(err)
	}
}

func TestKimiRefreshLockDirectoryLinked(t *testing.T) {
	source := fakeKimiUserHome(t)
	if err := os.MkdirAll(filepath.Join(source, "oauth"), 0700); err != nil {
		t.Fatal(err)
	}
	m := kimiHome(filepath.Join(t.TempDir(), "managed"), source)
	if err := m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"credentials", "oauth"} {
		got, err := os.Readlink(filepath.Join(m.Home, name))
		if err != nil || got != filepath.Join(source, name) {
			t.Errorf("%s is not shared: %q %v", name, got, err)
		}
	}
}
