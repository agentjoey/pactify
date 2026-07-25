package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A worker that fails producing NOTHING is env-class, and the escalation says
// so — that is the hook the fallback proposal hangs on.
func TestEscalationRecordsEnvClass(t *testing.T) {
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat/x", spec)

	notify := &recNotify{}
	// errRunner fails every stint and writes nothing → zero delivery → env.
	if err := Run(context.Background(), baseOpts(dir, errRunner{context.DeadlineExceeded}, &okExec{}, notify)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	files, _ := filepath.Glob(filepath.Join(dir, ".pact", "orchestrate", "escalation-*.md"))
	if len(files) == 0 {
		t.Fatalf("expected an escalation; notify=%v", notify.msgs)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "env") {
		t.Fatalf("escalation must record the failure class:\n%s", b)
	}
}
