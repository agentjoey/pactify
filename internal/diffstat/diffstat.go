// Package diffstat computes added/deleted line counts for a git range via
// `git diff --numstat`. Used to attribute code volume to a task's branch.
package diffstat

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Stat is the line delta for a git range.
type Stat struct {
	Added   int
	Deleted int
	Files   int
}

// runner runs a git command in dir and returns stdout. Injected so tests don't
// need a real repo.
type runner func(dir string, args ...string) (string, error)

func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

// NumStat returns the added/deleted/files for `git diff --numstat from..to` in
// repoDir. Binary files (numstat "-") count toward Files but contribute 0 lines.
func NumStat(repoDir, from, to string) (Stat, error) {
	return numStatWith(gitRun, repoDir, from, to)
}

func numStatWith(run runner, repoDir, from, to string) (Stat, error) {
	out, err := run(repoDir, "diff", "--numstat", from+".."+to)
	if err != nil {
		return Stat{}, fmt.Errorf("diffstat: git diff %s..%s: %w", from, to, err)
	}
	var s Stat
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		// numstat: "<added>\t<deleted>\t<path>"; binary → "-\t-\t<path>".
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 {
			continue
		}
		s.Files++
		if a, err := strconv.Atoi(f[0]); err == nil {
			s.Added += a
		}
		if d, err := strconv.Atoi(f[1]); err == nil {
			s.Deleted += d
		}
	}
	return s, nil
}
