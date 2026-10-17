package credentiallinks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixtureReplacementIsQuarantined(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	source := filepath.Join(outside, "fake-auth.json")
	private := filepath.Join(root, "auth.json")
	quarantine := filepath.Join(outside, "quarantine")
	if err := os.WriteFile(source, []byte("fake-old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, private); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFileLink(private, source, quarantine); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(private+".tmp", []byte("fake-new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(private+".tmp", private); err != nil {
		t.Fatal(err)
	}
	err := VerifyFileLink(private, source, quarantine)
	if err == nil || !strings.Contains(err.Error(), "CREDENTIAL LINK REPLACED") {
		t.Fatalf("replacement did not fail loudly: %v", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(quarantine, "*", "credential"))
	if err != nil || len(files) != 1 {
		t.Fatalf("preserved files=%v err=%v", files, err)
	}
	data, err := os.ReadFile(files[0]) // Synthetic fixture only.
	if err != nil || string(data) != "fake-new" {
		t.Fatalf("fake refresh lost: %q %v", data, err)
	}
	data, err = os.ReadFile(source)
	if err != nil || string(data) != "fake-old" {
		t.Fatal("fixture overwrote source")
	}
	info, err := os.Lstat(quarantine)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("quarantine is not private")
	}
}

func TestQuarantineRefusesUnsafeDirectoryAndRetainsFile(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "refresh")
	if err := os.WriteFile(private, []byte("fake-new"), 0600); err != nil {
		t.Fatal(err)
	}
	unsafe := filepath.Join(root, "unsafe")
	if err := os.Symlink(t.TempDir(), unsafe); err != nil {
		t.Fatal(err)
	}
	if _, err := Quarantine(private, unsafe); err == nil {
		t.Fatal("unsafe quarantine accepted")
	}
	if _, err := os.Lstat(private); err != nil {
		t.Fatal("replacement was lost after failed quarantine")
	}
}

func TestDefaultQuarantineUsesPactifyHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	if got, err := QuarantineDir(); err != nil || got != filepath.Join(home, "credential-quarantine") {
		t.Fatalf("quarantine must be persistent and test-isolated: %s", got)
	}
}

func TestDefaultQuarantineWithoutOverrideUsesPersistentHome(t *testing.T) {
	user := t.TempDir()
	t.Setenv("HOME", user)
	t.Setenv("PACTIFY_HOME", "")
	got, err := QuarantineDir()
	if err != nil || got != filepath.Join(user, ".pactify", "credential-quarantine") {
		t.Fatalf("default quarantine=%s err=%v", got, err)
	}
}

func TestRestorePreservesSourcePermissions(t *testing.T) {
	dir := t.TempDir()
	source, private := filepath.Join(dir, "source"), filepath.Join(dir, "private")
	if err := os.WriteFile(source, []byte("fake original"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := Inspect(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(private, []byte("fake refreshed"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0644); err != nil {
		t.Fatal(err)
	}
	location, err := Restore(private, source, before, filepath.Join(dir, "quarantine"))
	if err != nil || location != "" {
		t.Fatalf("restore=%s %v", location, err)
	}
	info, err := os.Lstat(source)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("source permissions were widened: %v %v", info, err)
	}
}

func TestRestoreChmodFailureQuarantines(t *testing.T) {
	dir := t.TempDir()
	source, private := filepath.Join(dir, "source"), filepath.Join(dir, "private")
	if err := os.WriteFile(source, []byte("fake original"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := Inspect(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(private, []byte("fake refreshed"), 0644); err != nil {
		t.Fatal(err)
	}
	location, err := restore(private, source, before, filepath.Join(dir, "quarantine"), func(string, os.FileMode) error { return errors.New("fake chmod failure") })
	if err != nil || location == "" {
		t.Fatalf("chmod failure must quarantine: %s %v", location, err)
	}
	after, err := Inspect(source)
	if err != nil || before != after {
		t.Fatal("source changed despite chmod failure")
	}
	if info, err := os.Lstat(location); err != nil || !info.Mode().IsRegular() {
		t.Fatal("refresh not preserved", err)
	}
}

func TestHostPlaintextFallbackIsQuarantined(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "home", ".claude", ".credentials.json")
	q := filepath.Join(outside, "credential-quarantine")
	if err := VerifyNoFallback(path, q); err != nil {
		t.Fatal("absent fallback must be harmless", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("synthetic host fallback"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyNoFallback(path, q); err == nil {
		t.Fatal("plaintext host fallback did not fail loudly")
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(q, "*", "credential"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("fallback lost: files=%v err=%v", paths, err)
	}
	// Metadata is sufficient: the original file was moved, not copied/opened.
	if info, err := os.Lstat(paths[0]); err != nil || !info.Mode().IsRegular() {
		t.Fatal("fallback not retained", err)
	}
}

func TestNestedQuarantineSurvivesFixtureRemoval(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	inner := filepath.Join(root, "pactify", "credential-quarantine")
	if err := os.MkdirAll(inner, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "fake-retained"), []byte("synthetic refresh"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFixtureQuarantine(inner, filepath.Join(outside, "quarantine")); err == nil {
		t.Fatal("inner quarantine must be reported")
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(outside, "quarantine", "*", "credential", "fake-retained"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("nested refresh lost: %v %v", paths, err)
	}
}
