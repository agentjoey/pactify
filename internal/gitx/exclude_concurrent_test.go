package gitx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Worktrees share info/exclude. Concurrent seat/snapshot setup must preserve
// every ignore rule, or the driver's own runtime files look like agent delivery.
func TestEnsureExcludedPreservesConcurrentRules(t *testing.T) {
	dir := tempRepo(t)
	path := filepath.Join(dir, ".git", "info", "exclude")
	if err := os.WriteFile(path, []byte("existing-rule\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const writers = 32
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs <- EnsureExcluded(dir, fmt.Sprintf("runtime-%d/", i))
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := -1; i < writers; i++ {
		want := "existing-rule"
		if i >= 0 {
			want = fmt.Sprintf("runtime-%d/", i)
		}
		if !strings.Contains("\n"+string(data), "\n"+want+"\n") {
			t.Errorf("concurrent ignore update lost %q", want)
		}
	}
}

// Git reads ignore rules during status/add while sibling worktrees update them.
// A truncate/rewrite can expose the driver's snapshot as committable delivery
// even when the final exclude file looks correct again.
func TestConcurrentExclusionKeepsRuntimeUntracked(t *testing.T) {
	dir := tempRepo(t)
	path := filepath.Join(dir, ".git", "info", "exclude")
	// Long existing content widens the truncate/rewrite window for the Git reader.
	prior := strings.Repeat("# existing local excludes\n", 32768) + ".pact/state-snapshot.json\n"
	if err := os.WriteFile(path, []byte(prior), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".pact"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pact", "state-snapshot.json"), []byte("runtime"), 0o600); err != nil {
		t.Fatal(err)
	}
	start, done := make(chan struct{}), make(chan struct{})
	errs := make(chan error, 4)
	var wg sync.WaitGroup
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			for j := 0; j < 32; j++ {
				if err := EnsureExcluded(dir, fmt.Sprintf("runtime-%d-%d/", i, j)); err != nil {
					errs <- err
					return
				}
			}
		}(i)
	}
	go func() { wg.Wait(); close(errs); close(done) }()
	close(start)
	var exposed string
	for {
		status, err := run(dir, "status", "--porcelain", "--untracked-files=all")
		if err != nil {
			exposed = err.Error()
		} else if status != "" {
			exposed = status
		}
		select {
		case <-done:
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			if exposed != "" {
				t.Fatalf("driver bookkeeping became committable agent delivery:\n%s", exposed)
			}
			return
		default:
		}
	}
}
