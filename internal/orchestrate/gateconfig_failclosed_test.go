package orchestrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// An unreadable pact log must FAIL CLOSED at gate resolution: resolveGate has
// no error channel, so it must return a command that always fails — never the
// type-default gate, which would silently strip a configured safety gate at
// merge time.
func TestResolveGateFailsClosedOnUnreadableLog(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	dir := newProject(t)

	logPath := filepath.Join(dir, ".pact", "log.jsonl")
	if err := os.WriteFile(logPath, []byte("{not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveGate(dir)
	if got == detectDefaultGate(dir) {
		t.Fatal("resolveGate degraded to the type-default gate on an unreadable log")
	}
	if !strings.Contains(got, "gate config unreadable") {
		t.Fatalf("fail-closed gate does not name the cause: %q", got)
	}
	// The returned command must itself fail when run: the gate blocks instead
	// of waving the merge through.
	if err := exec.Command("sh", "-c", got).Run(); err == nil {
		t.Fatalf("fail-closed gate command succeeded; it must always fail: %q", got)
	}
}
