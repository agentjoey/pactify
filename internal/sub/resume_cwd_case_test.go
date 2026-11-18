package sub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func caseAlias(t *testing.T, dir string) string {
	t.Helper()
	alias := filepath.Join(filepath.Dir(dir), strings.ToLower(filepath.Base(dir)))
	a, _ := os.Stat(dir)
	b, err := os.Stat(alias)
	if err != nil || !os.SameFile(a, b) {
		t.Skip("temporary filesystem is case-sensitive")
	}
	return alias
}

// Harnesses key native sessions by the cwd they were created in (kimi hashes
// the path), so a resume must run in the original spelling of the directory.
func TestResumeKeepsOriginalCwdSpelling(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	cwd := filepath.Join(t.TempDir(), "SessionDir")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	alias := caseAlias(t, cwd)
	if _, err := s.Create("previous", Request{Harness: "codex", Cwd: cwd, Access: "write"}); err != nil {
		t.Fatal(err)
	}
	finishWorktree(t, s, "previous", StatusSucceeded)
	next, err := s.CreateRun("next", Request{Harness: "codex", Cwd: alias, Access: "write"}, false, true)
	if err != nil || next.ResumeOf != "previous" {
		t.Fatalf("resume=%+v err=%v", next, err)
	}
	if next.Request.Cwd != cwd {
		t.Errorf("resumed cwd = %q, want the original spelling %q", next.Request.Cwd, cwd)
	}
}

func TestCanonicalResumeCwdIgnoresCaseSpelling(t *testing.T) {
	cwd := filepath.Join(t.TempDir(), "SessionDir")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	alias := caseAlias(t, cwd)
	if a, b := canonicalResumeCwd(cwd), canonicalResumeCwd(alias); a != b {
		t.Errorf("canonicalResumeCwd differs by spelling: %q vs %q", a, b)
	}
}

func TestCanonicalResumeCwdKeepsDistinctCaseDirectories(t *testing.T) {
	root := t.TempDir()
	upper, lower := filepath.Join(root, "Dir"), filepath.Join(root, "dir")
	if err := os.Mkdir(upper, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lower, 0700); err != nil {
		t.Skip("temporary filesystem is case-insensitive")
	}
	if canonicalResumeCwd(upper) == canonicalResumeCwd(lower) {
		t.Error("distinct directories were merged")
	}
}
