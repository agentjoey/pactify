package sub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedMirrorRefreshAndCleanup(t *testing.T) {
	source, home := t.TempDir(), filepath.Join(t.TempDir(), "managed")
	for _, name := range []string{"old", "keep"} {
		if err := os.Mkdir(filepath.Join(source, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	token := filepath.Join(source, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	if err := os.MkdirAll(filepath.Dir(token), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(token, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	m := agyHome(home, source)
	m.QuarantineDir = filepath.Join(t.TempDir(), "quarantine")
	if err := m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Check(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(filepath.Join(home, ".gemini")); err != nil || !info.IsDir() {
		t.Fatalf("private .gemini: %v %v", info, err)
	}
	if target, err := os.Readlink(filepath.Join(home, "keep")); err != nil || target != filepath.Join(source, "keep") {
		t.Fatal(target, err)
	}
	if err := os.Remove(filepath.Join(source, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "new"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, "old")); !os.IsNotExist(err) {
		t.Fatal("stale link remains", err)
	}
	if _, err := os.Readlink(filepath.Join(home, "new")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, filepath.Join(path, "escape")); err != nil {
		t.Fatal(err)
	}
	if warnings := m.Cleanup(); len(warnings) != 1 || !strings.Contains(warnings[0], "private copy") {
		t.Fatal(warnings)
	}
	if _, err := os.Lstat(token); err != nil {
		t.Fatal("source damaged", err)
	}
	if err := m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagedHomeRejectsNestedSymlinkAndAliases(t *testing.T) {
	for _, fault := range []string{"root", "ancestor", "source"} {
		t.Run(fault, func(t *testing.T) {
			source, home := t.TempDir(), filepath.Join(t.TempDir(), "managed")
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			m := agyHome(home, source)
			m.QuarantineDir = filepath.Join(t.TempDir(), "quarantine")
			switch fault {
			case "root":
				os.Remove(home)
				os.Symlink(source, home)
			case "ancestor":
				os.Symlink(source, filepath.Join(home, ".gemini"))
			case "source":
				m.Source = home
			}
			if err := m.Prepare(context.Background()); err == nil {
				t.Fatal("unsafe home accepted")
			}
			m.Cleanup()
			if info, err := os.Lstat(source); err != nil || !info.IsDir() {
				t.Fatal("source damaged", err)
			}
		})
	}
}

func TestAgyHarnessDeclaration(t *testing.T) {
	h, ok := Lookup("agy")
	if !ok || h.Kind != "antigravity" || h.Home == nil || h.configMode(false) != "minimal" || h.configMode(true) != "user" || h.Experimental == "" || h.NoReadOnly != "" {
		t.Fatalf("agy declaration: %+v", h)
	}
}

// A file the session writes at the top of its private HOME (new or rewritten
// by rename) is removed after the job and reported as such, not as a
// credential copy; the user's real HOME entry is left untouched.
func TestManagedMirrorCleanupReportsPrivateHomeWrites(t *testing.T) {
	source, home := t.TempDir(), filepath.Join(t.TempDir(), "managed")
	token := filepath.Join(source, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	if err := os.MkdirAll(filepath.Dir(token), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(token, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".gitconfig"), []byte("real"), 0600); err != nil {
		t.Fatal(err)
	}
	m := agyHome(home, source)
	m.QuarantineDir = filepath.Join(t.TempDir(), "quarantine")
	if err := m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Simulate a tool rewriting a mirrored dotfile by rename.
	if err := os.Remove(filepath.Join(home, ".gitconfig")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	warnings := m.Cleanup()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "private HOME") || strings.Contains(warnings[0], "credentials") {
		t.Fatalf("warnings: %v", warnings)
	}
	if _, err := os.Lstat(filepath.Join(home, ".gitconfig")); !os.IsNotExist(err) {
		t.Fatal("private write remains", err)
	}
	if data, err := os.ReadFile(filepath.Join(source, ".gitconfig")); err != nil || string(data) != "real" {
		t.Fatalf("real HOME entry changed: %q %v", data, err)
	}
}
