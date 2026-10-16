package serve

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAgyReviewVersionUsesManagedHomeAndModel(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	bin := t.TempDir()
	script := `#!/bin/sh
case "$*" in *"--model gemini-3.8-flash-low"*) ;; *) exit 91;; esac
case "$HOME" in "$PACTIFY_HOME"/*) ;; *) exit 92;; esac
printf 'agy 1.4.0\n'
`
	if e := os.WriteFile(filepath.Join(bin, "agy"), []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if v := probeVersion(context.Background(), "agy"); v != "1.4.0" {
		t.Fatalf("version=%q", v)
	}
}
