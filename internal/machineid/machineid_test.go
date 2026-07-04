package machineid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("PACTIFY_MACHINE_ID", "m-fixed")
	id, err := Load()
	if err != nil || id != "m-fixed" {
		t.Fatalf("env override: got %q err %v", id, err)
	}
}

func TestLoad_GeneratesPersistsAndReloads(t *testing.T) {
	t.Setenv("PACTIFY_MACHINE_ID", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	id1, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id1, "m-") || len(id1) < 6 {
		t.Fatalf("generated id looks wrong: %q", id1)
	}
	// Persisted to the expected path.
	if _, err := os.Stat(filepath.Join(home, ".config", "pactify", "machine-id")); err != nil {
		t.Fatalf("machine-id not persisted: %v", err)
	}
	// A second Load returns the SAME id (stable across restarts).
	id2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if id2 != id1 {
		t.Fatalf("id not stable: %q != %q", id1, id2)
	}
}
