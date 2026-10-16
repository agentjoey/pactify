package main

import (
	"os"
	"strings"
	"testing"
)

func TestSubAgyInvalidEffortBeforeLaunch(t *testing.T) {
	bin := buildPactify(t)
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "/usr/bin:/bin")
	out, code := runCLI(t, bin, t.TempDir(), "review", "sub", "run", "agy", "--effort", "maximum", "--wait", "0s")
	if code != 2 || !strings.Contains(out, "unsupported effort") {
		t.Fatalf("code=%d out=%s", code, out)
	}
	store := os.Getenv("PACTIFY_HOME") + "/jobs"
	if files, _ := os.ReadDir(store); len(files) != 0 {
		t.Fatal(files)
	}
}
