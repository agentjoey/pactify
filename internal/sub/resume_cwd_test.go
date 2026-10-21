package sub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutomaticResumeDirectoryIdentity(t *testing.T) {
	for _, spelling := range []string{"symlink", "case", "missing-exact", "different"} {
		t.Run(spelling, func(t *testing.T) {
			s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
			cwd := filepath.Join(t.TempDir(), "SessionDir")
			if err := os.Mkdir(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			lookup := cwd
			switch spelling {
			case "symlink":
				lookup = filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(cwd, lookup); err != nil {
					t.Fatal(err)
				}
			case "case":
				lookup = filepath.Join(filepath.Dir(cwd), "sessiondir")
				a, _ := os.Stat(cwd)
				b, err := os.Stat(lookup)
				if err != nil || !os.SameFile(a, b) {
					t.Skip("temporary filesystem is case-sensitive")
				}
			case "missing-exact":
				if err := os.Remove(cwd); err != nil {
					t.Fatal(err)
				}
			case "different":
				lookup = t.TempDir()
			}
			if _, err := s.Create("previous", Request{Harness: "codex", Cwd: cwd, Access: "write"}); err != nil {
				t.Fatal(err)
			}
			finishWorktree(t, s, "previous", StatusSucceeded)
			candidates, err := s.ResumeCandidates("codex", lookup)
			want := 1
			if spelling == "different" {
				want = 0
			}
			if err != nil || len(candidates) != want {
				t.Errorf("candidates=%v err=%v, want %d", candidates, err, want)
			}
			next, err := s.CreateRun("next", Request{Harness: "codex", Cwd: lookup, Access: "write"}, false, true)
			if want == 0 {
				if err == nil {
					t.Fatal("resumed a different directory")
				}
			} else if err != nil || next.ResumeOf != "previous" {
				t.Fatalf("resume=%+v err=%v", next, err)
			}
		})
	}
}
