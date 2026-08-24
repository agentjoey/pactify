package orchestrate

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// RunParams records how the CURRENT run was launched. It exists for exactly one
// reason: `pactify serve` resumes a paused run in a fresh subprocess and has no
// record of the original run's shape — its only run bookkeeping is orchRunning,
// an in-memory set of directories that carries no run parameters at all. Without
// this file a resume spawned from the dashboard (e.g. approving a fallback)
// silently downgrades a `--max-concurrency 3` run to serial: the operator
// approves one agent swap and, unannounced, the run stops driving three features
// at once (FALLBACK-PAR §2.8).
//
// The file is DELIBERATELY not under .pact/orchestrate/parallel/: that directory
// is glob-aggregated as one-file-per-feature by serve's parallel status handler,
// so a non-feature file there would surface as a phantom feature on the board.
//
// It is not deleted when a run ends — it describes how the LAST run went, and a
// resume happens precisely after the run has stopped. The next run overwrites it.
type RunParams struct {
	// MaxConcurrency is the run's feature-level concurrency cap; 1 means serial.
	MaxConcurrency int `json:"max_concurrency"`
}

func runParamsPath(dir string) string {
	return filepath.Join(dir, ".pact", "orchestrate", "run-params.json")
}

// WriteRunParams persists p atomically (temp + rename, same policy as
// writeStatus), so a concurrent reader never sees a half-written file.
func WriteRunParams(dir string, p RunParams) error {
	final := runParamsPath(dir)
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// ReadRunParams reads dir's run params. ok=false when the file is absent,
// unreadable, or not valid JSON — callers then treat the run as serial, which is
// exactly the behavior that predates this file (fail-safe: an unknown shape must
// never invent concurrency).
func ReadRunParams(dir string) (RunParams, bool) {
	b, err := os.ReadFile(runParamsPath(dir))
	if err != nil {
		return RunParams{}, false
	}
	var p RunParams
	if json.Unmarshal(b, &p) != nil {
		return RunParams{}, false
	}
	return p, true
}
