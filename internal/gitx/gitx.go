// Package gitx reads repository state for harness workers.
package gitx

import (
	"bytes"
	"os/exec"
	"strings"
)

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Successful stderr is diagnostic, not repository state. Changing xcrun
	// cache warnings must not keep refreshing the worker idle watchdog.
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	out, err := cmd.Output()
	if err != nil {
		out = append(out, diagnostics.Bytes()...)
	}
	return strings.TrimRight(string(out), "\n"), err
}

// TreeFingerprint captures the working tree's observable state — HEAD commit
// plus the porcelain status of every tracked/untracked change — as an opaque
// string. Two equal fingerprints mean nothing in the tree changed between the
// captures: no new commit, no file added/modified/deleted. (Ignored files are
// invisible on both sides, so runtime noise routed through .git/info/exclude
// never perturbs the fingerprint.)
func TreeFingerprint(dir string) string {
	head, err := run(dir, "rev-parse", "HEAD")
	if err != nil {
		head = ""
	}
	// A read probe must not run repo-local fsmonitor code or refresh the index.
	status, err := run(dir, "--no-optional-locks", "-c", "core.fsmonitor=false", "status", "--porcelain")
	if err != nil {
		status = ""
	}
	return strings.TrimSpace(head) + "\n" + status
}
