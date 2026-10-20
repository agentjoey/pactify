package testenv

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Isolate runs a package's tests with a disposable PACTIFY_HOME. Product code
// falls back to ~/.pactify when PACTIFY_HOME is unset, so a plain `go test`
// would otherwise write jobs, caches and quarantined credentials into the
// developer's real store. An explicit PACTIFY_HOME is kept (live e2e and golden
// recording use persistent homes), unless it names that real store.
func Isolate(m *testing.M) int { return isolate(m.Run) }

func isolate(run func() int) int {
	home := os.Getenv("PACTIFY_HOME")
	if home != "" && !isUserStore(home) {
		return run()
	}
	// Name the home without creating it: helper subprocesses re-run this
	// TestMain with a scrubbed environment and exit from inside a test, so a
	// directory created here would outlive them. Product code creates what it uses.
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		fmt.Fprintln(os.Stderr, "testenv: name PACTIFY_HOME:", err)
		return 1
	}
	dir := filepath.Join(os.TempDir(), "pactify-test-home-"+hex.EncodeToString(suffix[:]))
	defer os.RemoveAll(dir)
	if home != "" {
		fmt.Fprintf(os.Stderr, "testenv: PACTIFY_HOME=%s is the real user store; tests use %s instead\n", home, dir)
	}
	os.Setenv("PACTIFY_HOME", dir)
	return run()
}

func isUserStore(home string) bool {
	user, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	return strings.EqualFold(canonical(home), canonical(filepath.Join(user, ".pactify")))
}

func canonical(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}
