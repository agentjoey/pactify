package sub

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// An interrupt during one harness marks its unfinished checks CANCELED, keeps
// finished results, and does not start later harnesses.
func TestDoctorInterruptMarksChecksCanceled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	codex, _ := Lookup("codex")
	codex.Version = func(context.Context) (string, error) { return "1", nil }
	kimi, _ := Lookup("kimi")
	var lines []string
	runs := 0
	reports, err := runDoctor(ctx, Store{Root: filepath.Join(home, "jobs")}, []Harness{codex, kimi}, doctorDeps{
		tempParent: t.TempDir(),
		lookPath:   func(string) (string, error) { return "test", nil },
		progress:   func(line string) { lines = append(lines, line) },
		run: func(ctx context.Context, _ Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
			runs++
			if id == "write" {
				cancel() // Ctrl-C while the first model call runs.
			}
			return Result{Status: StatusCanceled}, ctx.Err()
		},
	})
	if err != nil || len(reports) != 2 {
		t.Fatalf("reports=%+v err=%v", reports, err)
	}
	for _, c := range reports[0].Checks {
		if c.Status == "FAIL" {
			t.Fatalf("interrupted check reported as FAIL: %+v", c)
		}
	}
	if !hasCheck(reports[0], "Write access", doctorCanceled) || !hasCheck(reports[0], "Installed", "PASS") || reports[0].Passed() {
		t.Fatalf("codex report=%+v", reports[0])
	}
	if !hasCheck(reports[1], "Doctor", doctorCanceled) || len(reports[1].Checks) != 1 {
		t.Fatalf("kimi should not run: %+v", reports[1])
	}
	if len(lines) == 0 || !strings.Contains(lines[0], "codex: checking write access") {
		t.Fatalf("progress=%q", lines)
	}
}

func TestDoctorPrintsProgressAndInterruptHint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, progress strings.Builder
	passed, err := Doctor(ctx, Store{Root: filepath.Join(home, "jobs")}, []string{"fake"}, &out, DoctorOptions{Progress: &progress})
	if passed || err == nil || !strings.Contains(err.Error(), "interrupted; rerun") {
		t.Fatalf("passed=%t err=%v", passed, err)
	}
	if !strings.Contains(progress.String(), "real model calls") || strings.Contains(out.String(), "real model calls") {
		t.Fatalf("progress=%q out=%q", progress.String(), out.String())
	}
	if !strings.Contains(out.String(), "CANCELED") || strings.Contains(out.String(), "FAIL") {
		t.Fatalf("out=%s", out.String())
	}
}

func hasCheck(r DoctorReport, name, status string) bool {
	for _, c := range r.Checks {
		if c.Name == name && c.Status == status {
			return true
		}
	}
	return false
}
