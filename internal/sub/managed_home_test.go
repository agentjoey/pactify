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
	if !ok || h.Kind != "antigravity" || h.Home == nil || h.configMode(false) != "minimal" || h.configMode(true) != "user" || h.Experimental != "" || h.Warning != "agy has no OS sandbox for writes" || h.NoReadOnly != "" {
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

// agy can keep its login in the OS keyring, so a missing token file must not
// block the job; a stale link to it is removed, and the file is linked again
// once it exists. Required links still fail when their source is missing.
func TestManagedHomeOptionalCredentialLink(t *testing.T) {
	source, home := t.TempDir(), filepath.Join(t.TempDir(), "managed")
	m := agyHome(home, source)
	m.QuarantineDir = filepath.Join(t.TempDir(), "quarantine")
	path := filepath.Join(home, agyTokenPath)
	token := filepath.Join(source, agyTokenPath)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(token, path); err != nil {
		t.Fatal(err)
	}
	if err := m.Prepare(context.Background()); err != nil {
		t.Fatalf("missing optional token blocked prepare: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("dangling token link kept: %v", err)
	}
	if err := m.Check(); err != nil {
		t.Fatal(err)
	}
	if warnings := m.Cleanup(); len(warnings) != 0 {
		t.Fatal(warnings)
	}

	// agy writes a refreshed token here when the keyring save fails. With no
	// source to return it to, cleanup discards it instead of quarantining it.
	if err := m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("refreshed"), 0600); err != nil {
		t.Fatal(err)
	}
	if warnings := m.Cleanup(); len(warnings) != 0 {
		t.Fatalf("refreshed copy warnings: %v", warnings)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("refreshed copy kept: %v", err)
	}
	if _, err := os.Stat(m.QuarantineDir); !os.IsNotExist(err) {
		t.Fatalf("refreshed copy quarantined: %v", err)
	}
	// A copy left by a process that exited before cleanup is dropped on setup.
	if err := os.WriteFile(path, []byte("leftover"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("leftover copy kept: %v", err)
	}
	if err := m.Check(); err != nil {
		t.Fatal(err)
	}
	m.Cleanup()

	if err := os.MkdirAll(filepath.Dir(token), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(token, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(path); err != nil || target != token {
		t.Fatalf("token link: %q %v", target, err)
	}
	if err := m.Check(); err != nil {
		t.Fatal(err)
	}
	m.Cleanup()

	required := opencodeData(filepath.Join(t.TempDir(), "managed"), t.TempDir())
	required.QuarantineDir = filepath.Join(t.TempDir(), "quarantine")
	if err := required.Prepare(context.Background()); err == nil || !strings.Contains(err.Error(), "login first") {
		t.Fatalf("missing required credential accepted: %v", err)
	}
	required.Cleanup()
}
