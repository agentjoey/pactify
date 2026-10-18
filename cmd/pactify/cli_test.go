package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "pactify")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/pactify")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd() // .../cmd/pactify
	return filepath.Dir(filepath.Dir(wd))
}
