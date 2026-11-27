package sub

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

// outsideFixture gives a job a private HOME so the watched home files belong to the test.
func outsideFixture(t *testing.T) (Store, *fakeSession, *fakeBackend, string, string) {
	t.Helper()
	store, s, b := workerFixture(t, "write")
	home := t.TempDir()
	t.Setenv("HOME", home)
	write(t, home, ".zshrc", "export A=1\n")
	mkdir(t, filepath.Join(home, ".ssh"))
	write(t, home, ".ssh/known_hosts", "host key\n")
	st, err := store.Load("j1")
	if err != nil {
		t.Fatal(err)
	}
	return store, s, b, home, st.Request.Cwd
}

func finishScript(mutate func()) func(*fakeSession) {
	return func(s *fakeSession) {
		mutate()
		s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "done"}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
}

func TestRunJobReportsWritesOutsideTheWorkspace(t *testing.T) {
	store, s, b, home, repo := outsideFixture(t)
	git := filepath.Join(repo, ".git")
	s.script = finishScript(func() {
		mkdir(t, filepath.Join(git, "hooks"))
		write(t, git, "hooks/pre-commit", "#!/bin/sh\ncurl example.invalid\n")
		f, _ := os.OpenFile(filepath.Join(git, "config"), os.O_APPEND|os.O_WRONLY, 0)
		_, _ = f.WriteString("[core]\n\thooksPath = /tmp/elsewhere\n")
		_ = f.Close()
		write(t, home, ".zshrc", "export A=2\n")
		write(t, home, ".ssh/known_hosts", "host key\nnew host key\n")
	})
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	want := []OutsideChange{
		{filepath.Join(home, ".zshrc"), "modified"},
		{filepath.Join(git, "config"), "modified"},
		{filepath.Join(git, "hooks/pre-commit"), "added"},
	}
	if !reflect.DeepEqual(canonicalChanges(r.OutsideChanges), canonicalChanges(want)) {
		t.Fatalf("outside changes = %+v", r.OutsideChanges)
	}
	if !contains(r.Warnings, outsideChangesPrefix) || !contains(r.Warnings, "pre-commit") {
		t.Fatalf("warnings = %q", r.Warnings)
	}
	st, err := store.Load("j1")
	if err != nil {
		t.Fatal(err)
	}
	row := store.Summarize(st, nil, time.Now())
	if len(row.Attention) != 1 || row.Attention[0].Kind != AttentionOutsideChanges || row.Attention[0].Message != "3 file(s) outside the workspace changed during the job" {
		t.Fatalf("attention = %+v", row.Attention)
	}
	if err := store.CheckAction(st, "dismiss"); err != nil {
		t.Fatalf("outside changes must be dismissable: %v", err)
	}
}

func TestRunJobWithoutOutsideWritesReportsNothing(t *testing.T) {
	store, s, b, _, repo := outsideFixture(t)
	s.script = finishScript(func() { write(t, repo, "inside.txt", "ok\n") })
	r, err := RunJob(context.Background(), store, "j1", newFakeHarness(b), WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusSucceeded || len(r.OutsideChanges) != 0 || contains(r.Warnings, "outside the workspace") {
		t.Fatalf("result = %+v", r)
	}
	st, _ := store.Load("j1")
	if st.OutsideBaseline == nil || len(st.OutsideBaseline.Entries) == 0 {
		t.Fatal("baseline was not persisted for recovery")
	}
}

func TestOutsideCheckSkipsOSSandboxedHarnesses(t *testing.T) {
	for harness, want := range map[string]bool{"codex": false, "dsh": false, "agy": true, "kimi": true, "opencode": true} {
		if got := requiresOutsideBaseline(Request{Harness: harness}); got != want {
			t.Errorf("%s: requiresOutsideBaseline = %v", harness, got)
		}
	}
}

func TestOutsideCheckWithoutBaselineWarnsOnlyForStartedJobs(t *testing.T) {
	r := Result{}
	checkOutside(&State{Request: Request{Harness: "agy"}}, &r)
	if len(r.Warnings) != 0 {
		t.Fatalf("never-started job warned: %q", r.Warnings)
	}
	checkOutside(&State{Request: Request{Harness: "agy"}, StartedAt: time.Now()}, &r)
	if !reflect.DeepEqual(r.Warnings, []string{outsideUncheckedWarning}) {
		t.Fatalf("warnings = %q", r.Warnings)
	}
}

func TestCaptureOutsideDoesNotFollowLinks(t *testing.T) {
	home, target := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	write(t, target, "secret", "a\n")
	if err := os.Symlink(target, filepath.Join(home, ".ssh")); err != nil {
		t.Fatal(err)
	}
	snap, err := captureOutside(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for path := range snap.Entries {
		if strings.Contains(path, "secret") {
			t.Fatalf("followed link: %v", snap.Entries)
		}
	}
	if !strings.HasPrefix(snap.Entries[filepath.Join(home, ".ssh")], "link:") {
		t.Fatalf("link not recorded: %v", snap.Entries)
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

// canonicalChanges resolves /var vs /private/var so paths compare on macOS.
func canonicalChanges(in []OutsideChange) []OutsideChange {
	out := make([]OutsideChange, len(in))
	for i, c := range in {
		dir, err := filepath.EvalSymlinks(filepath.Dir(c.Path))
		if err != nil {
			dir = filepath.Dir(c.Path)
		}
		out[i] = OutsideChange{filepath.Join(dir, filepath.Base(c.Path)), c.Change}
	}
	sortChanges(out)
	return out
}
