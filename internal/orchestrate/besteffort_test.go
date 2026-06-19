package orchestrate

import (
	"fmt"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) { return 0, fmt.Errorf("boom") }

// A failing live-stream sink must never surface its error: io.MultiWriter aborts
// the whole write on any sub-writer error, which would stall the child's stdout
// pipe and hang the agent. bestEffortWriter guarantees (len(p), nil).
func TestBestEffortWriterSwallowsErrors(t *testing.T) {
	n, err := bestEffortWriter{failingWriter{}}.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("err = %v, want nil (sink errors must not propagate)", err)
	}
	if n != 5 {
		t.Fatalf("n = %d, want 5", n)
	}
}
