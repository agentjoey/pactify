package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunParamsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadRunParams(dir); ok {
		t.Fatal("no file yet → not ok")
	}
	if err := WriteRunParams(dir, RunParams{MaxConcurrency: 3}); err != nil {
		t.Fatal(err)
	}
	p, ok := ReadRunParams(dir)
	if !ok || p.MaxConcurrency != 3 {
		t.Fatalf("ReadRunParams = %+v ok=%v, want 3", p, ok)
	}
	// Deliberately NOT under parallel/: that dir is glob-aggregated one file per
	// feature, and a non-feature file there becomes a phantom feature.
	if _, err := os.Stat(filepath.Join(dir, ".pact", "orchestrate", "run-params.json")); err != nil {
		t.Fatalf("run-params must live beside status.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".pact", "orchestrate", "parallel")); !os.IsNotExist(err) {
		t.Fatal("run-params must not be written into the parallel status dir")
	}
}

// Fail-safe: a missing or corrupt file reads as "unknown", and every caller then
// treats the run as serial — exactly the behavior that predates this file.
func TestReadRunParamsFailsSafe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".pact", "orchestrate", "run-params.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if p, ok := ReadRunParams(dir); ok || p.MaxConcurrency != 0 {
		t.Fatalf("invalid JSON must read as unknown, got %+v ok=%v", p, ok)
	}
}

// A parallel run records its concurrency so a resume spawned by another process
// can reproduce it instead of silently downgrading the run to serial (§2.8).
func TestRunParallelRecordsItsConcurrency(t *testing.T) {
	bindFallbackRoles(t)
	dir := twoFeatureProject(t)
	if err := RunParallel(context.Background(), parOpts(dir, parEnvFailRunner{context.DeadlineExceeded}, &syncNotify{}, 2, t)); err != nil {
		t.Fatalf("RunParallel: %v", err)
	}
	p, ok := ReadRunParams(dir)
	if !ok || p.MaxConcurrency != 2 {
		t.Fatalf("a parallel run must record max_concurrency, got %+v ok=%v", p, ok)
	}
}

// A serial run records max_concurrency=1, so a previous parallel run's params
// can never make a later serial run resume as parallel.
func TestSerialRunOverwritesStaleRunParams(t *testing.T) {
	bindFallbackRoles(t)
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat-x", spec)
	if err := WriteRunParams(dir, RunParams{MaxConcurrency: 7}); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), baseOpts(dir, errRunner{context.DeadlineExceeded}, &okExec{}, &recNotify{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	p, ok := ReadRunParams(dir)
	if !ok || p.MaxConcurrency != 1 {
		t.Fatalf("a serial run must record itself as serial, got %+v ok=%v", p, ok)
	}
}
