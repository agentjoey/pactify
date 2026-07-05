package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// escalationFilename must embed feature+task so a human scanning the directory
// can tell which run an escalation is about without opening it — the ambiguity
// that let a days-old, unrelated escalation get mistaken for the current run's
// state during the 2026-07-05 dogfood (P1).
func TestEscalationFilename(t *testing.T) {
	cases := []struct{ feature, task, ts, want string }{
		{"f1", "t1", "20260613-000000", "escalation-f1-t1-20260613-000000.md"},
		{"f1", "", "20260613-000000", "escalation-f1-20260613-000000.md"},
		{"", "t1", "20260613-000000", "escalation-t1-20260613-000000.md"},
		{"", "", "20260613-000000", "escalation-20260613-000000.md"},
	}
	for _, c := range cases {
		if got := escalationFilename(c.feature, c.task, c.ts); got != c.want {
			t.Errorf("escalationFilename(%q,%q,%q) = %q, want %q", c.feature, c.task, c.ts, got, c.want)
		}
	}
}

// archiveEscalationsForFeature moves ONLY the named feature's own escalation
// files into archive/, leaving other features' (and feature-less) escalations
// untouched in the live directory. Matching is by the record's `## Feature`
// content section, so hyphenated feature ids can never prefix-collide in the
// filename (feature "fa" must not sweep feature "fa-old"'s files).
func TestArchiveEscalationsForFeature(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, ".pact", "orchestrate")
	// Write records through the REAL producer so the content format (the
	// ## Feature section the matcher parses) can never drift from production.
	mustWrite := func(ts, feature, task string) string {
		t.Helper()
		p, err := writeEscalation(dir, ts, feature, task, "reason", "ev", "sug")
		if err != nil {
			t.Fatal(err)
		}
		return filepath.Base(p)
	}
	faT1 := mustWrite("20260613-000000", "fa", "t1")      // fa's own — should archive
	faT2 := mustWrite("20260613-000001", "fa", "t2")      // fa's own — should archive
	fbT1 := mustWrite("20260613-000002", "fb", "t1")      // a DIFFERENT feature — must stay
	faOld := mustWrite("20260613-000003", "fa-old", "t9") // prefix-COLLIDING feature — must stay
	noFeat := mustWrite("20260613-000004", "", "t1")      // feature-less — must stay
	// A pre-P1 record (no ## Feature section at all) — must stay.
	legacy := "escalation-20260101-000000.md"
	if err := os.WriteFile(filepath.Join(outDir, legacy), []byte("# Escalation\n\n## Task\n\nt0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	archiveEscalationsForFeature(dir, "fa")

	stillLive := func(name string) bool {
		_, err := os.Stat(filepath.Join(outDir, name))
		return err == nil
	}
	archived := func(name string) bool {
		_, err := os.Stat(filepath.Join(outDir, "archive", name))
		return err == nil
	}
	if stillLive(faT1) || !archived(faT1) {
		t.Error("fa's t1 escalation should have been archived")
	}
	if stillLive(faT2) || !archived(faT2) {
		t.Error("fa's t2 escalation should have been archived")
	}
	if !stillLive(fbT1) {
		t.Error("a DIFFERENT feature's escalation must NOT be touched")
	}
	if !stillLive(faOld) {
		t.Error("feature fa-old's escalation must NOT be swept by feature fa (filename prefix collision)")
	}
	if !stillLive(noFeat) {
		t.Error("a feature-less escalation must NOT be touched")
	}
	if !stillLive(legacy) {
		t.Error("a legacy (pre-P1, no Feature section) escalation must NOT be touched")
	}
}

// End-to-end (the actual P1 scenario): feature fa ships → its own prior
// escalation is archived out of the live directory, while a live escalation
// for an UNRELATED feature (pre-existing debris from a different run, exactly
// like the days-old files that caused the 2026-07-05 dogfood confusion) is left
// alone and clearly distinguishable by name — a human (or driver) scanning the
// live directory now only ever sees escalations that are actually still open.
func TestLoopArchivesOwnEscalationOnShipLeavesOthersAlone(t *testing.T) {
	dir := newProject(t)
	// Pre-existing, unrelated debris from a DIFFERENT feature/day.
	outDir := filepath.Join(dir, ".pact", "orchestrate")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "escalation-old-feature-t9-20260101-000000.md"), []byte("stale debris"), 0o644); err != nil {
		t.Fatal(err)
	}

	s1 := writeSpec(t, dir, "t1", "go test ./...")
	assign(t, dir, "t1", "f", "feat/x", s1)
	runner := newFakeRunner(t, dir)
	runner.alwaysChanges = true // first escalate on rework limit...
	opts := baseOpts(dir, runner, &okExec{}, &recNotify{})
	opts.Th.MaxRework = 1
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run (rework escalation): %v", err)
	}
	// The rework escalation for "f" must exist now, named for "f".
	if _, err := os.Stat(findEscalation(t, dir, fixedNow())); err != nil {
		t.Fatalf("expected an escalation for feature f: %v", err)
	}

	// Now let it actually ship: reviewer accepts.
	runner.alwaysChanges = false
	runner.reviewSeen = map[string]int{}
	if err := Run(context.Background(), baseOpts(dir, runner, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run (ship): %v", err)
	}
	if got := featureStatus(t, dir, "f"); got != "shipped" {
		t.Fatalf("feature status = %q, want shipped", got)
	}

	// "f"'s own escalation is archived — no longer live.
	if _, err := os.Stat(filepath.Join(outDir, "archive")); err != nil {
		t.Fatal("archive dir should exist after shipping a feature with a prior escalation")
	}
	liveEntries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range liveEntries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), "escalation-f-") {
			t.Errorf("shipped feature's own escalation %q should have been archived, not left live", e.Name())
		}
	}
	// The unrelated OLD debris is untouched — still live, still clearly a
	// different feature by name (never confusable with "f"'s run).
	if _, err := os.Stat(filepath.Join(outDir, "escalation-old-feature-t9-20260101-000000.md")); err != nil {
		t.Error("an unrelated feature's escalation must be left alone, not swept up by this feature's ship")
	}
}

func TestEscalate_WriteEscalation(t *testing.T) {
	dir := t.TempDir()
	ts := "20260613-140530"
	feature := "f1"
	task := "t2"
	reason := "rework limit reached (3 rounds of changes_requested)"
	evidence := "--- FAIL: TestRelay\nexpected 200, got 500"
	suggestion := "review the spec's acceptance command; the handler may need a nil check"

	path, err := writeEscalation(dir, ts, feature, task, reason, evidence, suggestion)
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
	// The filename ALSO carries feature+task (P1): a human scanning the
	// directory must be able to tell which run an escalation is about without
	// opening it.
	if want := "escalation-" + feature + "-" + task + "-" + ts + ".md"; base != want {
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

	path, err := writeEscalation(dir, "20260613-000000", "f1", "t1", "stuck", "ev", "sug")
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
