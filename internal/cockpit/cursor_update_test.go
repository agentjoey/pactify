package cockpit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A self-update replaces the executable; the next resolution must see the new build.
func TestResolveCursorBinaryReprobesAfterUpdate(t *testing.T) {
	bin := t.TempDir()
	write := func(version string) {
		script := "#!/bin/sh\ncase \"$1\" in\n--version) echo " + version + " ;;\nesac\n"
		if err := os.WriteFile(filepath.Join(bin, "cursor-agent"), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	write("2026.11.17-f532c45")
	if _, v, err := ResolveCursorBinary(context.Background()); err != nil || v != "2026.11.17-f532c45" {
		t.Fatalf("%q %v", v, err)
	}
	write("2026.12.01-abcdef12")
	if _, v, err := ResolveCursorBinary(context.Background()); err != nil || v != "2026.12.01-abcdef12" {
		t.Fatalf("stale version after update: %q %v", v, err)
	}
}
