package orchestrate

import (
	"io"
	"os"
	"path/filepath"
)

// StreamPath is where a task's live agent-output stream is mirrored: one append
// log per task under the repo's .pact/orchestrate/streams/. serve tails this to
// feed the Live lane terminal.
func StreamPath(repoDir, task string) string {
	return filepath.Join(repoDir, ".pact", "orchestrate", "streams", task+".log")
}

// OpenStreamSink opens (creating dirs) the per-task stream log for append. The
// returned writer is spliced into the agent's stdout tee; callers Close it when
// the stint ends. Best-effort: a returned error means "no live stream this run",
// never a run failure.
func OpenStreamSink(repoDir, task string) (io.WriteCloser, error) {
	p := StreamPath(repoDir, task)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}
