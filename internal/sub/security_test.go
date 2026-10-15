package sub

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityManagedHomeConcurrentUsers(t *testing.T) {
	source, home := t.TempDir(), filepath.Join(t.TempDir(), "managed")
	if err := os.WriteFile(filepath.Join(source, "credential"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	h := &harnessHome{Env: "HOME", Source: func() (string, error) { return source, nil }, Managed: func() (string, error) { return home, nil }, Layout: func(home, source string) *managedHome {
		return &managedHome{Name: "fixture", Home: home, Source: source, Mirror: true, Links: map[string]homeLink{"credential": {Target: filepath.Join(source, "credential")}}}
	}}
	prepare := func() *managedHome {
		t.Helper()
		_, _, _, layouts, err := prepareHarnessHomes(context.Background(), h, "", nil, true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { layouts[0].Cleanup() })
		return layouts[0]
	}
	a, b := prepare(), prepare()
	for _, name := range []string{"private", "credential"} {
		path := filepath.Join(home, name)
		if name == "credential" {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(path, []byte("private"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a.Cleanup()
	for _, name := range []string{"private", "credential"} {
		if _, err := os.Lstat(filepath.Join(home, name)); err != nil {
			t.Fatalf("A cleanup removed B's %s: %v", name, err)
		}
	}
	c := prepare()
	for _, name := range []string{"private", "credential"} {
		info, err := os.Lstat(filepath.Join(home, name))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("concurrent setup replaced %s: %v", name, err)
		}
	}
	b.Cleanup()
	c.Cleanup()
	for _, name := range []string{"private", "credential"} {
		if _, err := os.Lstat(filepath.Join(home, name)); !os.IsNotExist(err) {
			t.Fatalf("last cleanup retained %s: %v", name, err)
		}
	}
	// A stale credential from a killed worker must be replaced by the next exclusive setup.
	if err := os.WriteFile(filepath.Join(home, "credential"), []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	d := prepare()
	defer d.Cleanup()
	if _, err := os.Readlink(filepath.Join(home, "credential")); err != nil {
		t.Fatal("exclusive setup retained credential copy", err)
	}
}

func TestSecurityLogWindowAndPartialBound(t *testing.T) {
	store := Store{Root: t.TempDir()}
	st, err := store.Create("logs", Request{Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	st.Status = StatusSucceeded
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	path := store.LogPath(st.ID)
	if err := os.WriteFile(path, []byte("outside-window\n"+strings.Repeat("x", 2<<20)+"\nrecent\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := store.Logs(context.Background(), st.ID, &out, LogOptions{Tail: 100, Follow: true, ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "outside-window") || !strings.Contains(out.String(), "recent") {
		t.Fatalf("snapshot outside window: %.200s", out.String())
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 2<<20)), 0600); err != nil {
		t.Fatal(err)
	}
	r := &logReader{path: path}
	defer r.close()
	if err := r.drain(func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(r.pending) > 64<<10 {
		t.Fatalf("unbounded half-line: %d", len(r.pending))
	}
	for i := 0; i < 5; i++ {
		if err := r.drain(func(string) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\nafter\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	if err := r.drain(func(s string) error { lines = append(lines, s); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 || lines[len(lines)-1] != "after" {
		t.Fatal(lines)
	}
}
