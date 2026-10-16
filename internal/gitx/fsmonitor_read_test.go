package gitx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTreeFingerprintDoesNotExecuteRepoHelpers(t *testing.T) {
	repo := tempRepo(t)
	marker := filepath.Join(t.TempDir(), "executed")
	helper := filepath.Join(t.TempDir(), "fsmonitor")
	if e := os.WriteFile(helper, []byte("#!/bin/sh\nprintf unsafe >> '"+marker+"'\nprintf '\\0'\n"), 0700); e != nil {
		t.Fatal(e)
	}
	if out, e := run(repo, "config", "core.fsmonitor", helper); e != nil {
		t.Fatalf("config: %s %v", out, e)
	}
	_ = TreeFingerprint(repo)
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("idle fingerprint executed repository fsmonitor", e)
	}
}
