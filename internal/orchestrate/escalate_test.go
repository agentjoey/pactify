package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEscalate_WriteEscalation(t *testing.T) {
	dir := t.TempDir()
	ts := "20260613-140530"
	task := "t2"
	reason := "rework limit reached (3 rounds of changes_requested)"
	evidence := "--- FAIL: TestRelay\nexpected 200, got 500"
	suggestion := "review the spec's acceptance command; the handler may need a nil check"

	path, err := writeEscalation(dir, ts, task, reason, evidence, suggestion)
	if err != nil {
		t.Fatalf("writeEscalation: unexpected error: %v", err)
	}

	// Path lives under <dir>/.pact/orchestrate/ and the filename carries ts.
	wantDir := filepath.Join(dir, ".pact", "orchestrate")
	if got := filepath.Dir(path); got != wantDir {
		t.Fatalf("writeEscalation: path dir = %q, want %q", got, wantDir)
	}
	base := filepath.Base(path)
	if !strings.Contains(base, ts) {
		t.Fatalf("writeEscalation: filename %q should contain ts %q", base, ts)
	}
	if want := "escalation-" + ts + ".md"; base != want {
		t.Fatalf("writeEscalation: filename = %q, want %q", base, want)
	}

	// File exists on disk.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("writeEscalation: stat written file: %v", err)
	}

	// Content carries all four field values.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("writeEscalation: read written file: %v", err)
	}
	content := string(raw)
	for name, val := range map[string]string{
		"task":       task,
		"reason":     reason,
		"evidence":   evidence,
		"suggestion": suggestion,
	} {
		if !strings.Contains(content, val) {
			t.Errorf("writeEscalation: content missing %s value %q\n--- content ---\n%s", name, val, content)
		}
	}
}

func TestEscalate_WriteEscalation_CreatesDir(t *testing.T) {
	// The tempdir does NOT yet contain .pact/orchestrate; writeEscalation must
	// create the full path with os.MkdirAll.
	dir := t.TempDir()
	orchDir := filepath.Join(dir, ".pact", "orchestrate")
	if _, err := os.Stat(orchDir); !os.IsNotExist(err) {
		t.Fatalf("precondition: %q should not exist yet (err=%v)", orchDir, err)
	}

	path, err := writeEscalation(dir, "20260613-000000", "t1", "stuck", "ev", "sug")
	if err != nil {
		t.Fatalf("writeEscalation: unexpected error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("writeEscalation: written file not found after auto-mkdir: %v", err)
	}
}

// fakeNotifier records the messages it is handed, for deterministic assertions.
type fakeNotifier struct {
	messages []string
}

func (f *fakeNotifier) Notify(message string) {
	f.messages = append(f.messages, message)
}

func TestEscalate_NotifierReceivesMessage(t *testing.T) {
	fn := &fakeNotifier{}
	var n Notifier = fn
	n.Notify("escalation: task t2 stuck")

	if len(fn.messages) != 1 {
		t.Fatalf("Notify: want 1 message recorded, got %d", len(fn.messages))
	}
	if fn.messages[0] != "escalation: task t2 stuck" {
		t.Fatalf("Notify: recorded %q, want %q", fn.messages[0], "escalation: task t2 stuck")
	}
}

func TestEscalate_StdoutNotifier_DoesNotPanic(t *testing.T) {
	// StdoutNotifier is the default production notifier; exercising it must be
	// side-effect-safe (writes to stdout) and satisfy the Notifier interface.
	var n Notifier = StdoutNotifier{}
	n.Notify("escalation written")
}
