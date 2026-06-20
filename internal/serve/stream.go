package serve

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/agentjoey/pactify/internal/orchestrate"
)

// handleAgentStream tails a task's live agent-output log over SSE: backfill the
// existing tail on connect, then poll for appended lines. Read-only, best-effort
// — a missing stream file just yields an open, empty stream (the task may not be
// running yet). Modeled on handleEvents (see api.go).
func (s *Server) handleAgentStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.pmu.RLock()
	p, known := s.projects[id]
	s.pmu.RUnlock()
	if !known {
		http.Error(w, "unknown project", http.StatusNotFound)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	lp := orchestrate.StreamPath(p.Path, r.PathValue("task"))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "retry: 3000\n: ok\n\n")
	fl.Flush()

	// Backfill from a single read so the resume offset is the exact byte boundary
	// of the bytes we delivered — never a separate Stat that could race an append
	// in between and re-deliver those bytes on the first tick. off lands on the
	// last newline; any trailing partial line waits for drainStream to emit it whole.
	var off int64
	if data, err := os.ReadFile(lp); err == nil {
		lines, consumed := completeLines(data)
		if len(lines) > eventBackfill {
			lines = lines[len(lines)-eventBackfill:]
		}
		for _, line := range lines {
			io.WriteString(w, "event: stream\ndata: "+line+"\n\n")
		}
		off = int64(consumed)
		fl.Flush()
	}

	tick := time.NewTicker(700 * time.Millisecond)
	defer tick.Stop()
	hb := time.NewTicker(sseHeartbeat)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-hb.C:
			io.WriteString(w, ": ping\n\n")
			fl.Flush()
		case <-tick.C:
			off = drainStream(w, lp, off)
			fl.Flush()
		}
	}
}

// drainStream writes any bytes appended past off as SSE frames and returns the
// new offset. Re-reads from 0 if the file shrank (truncation/branch swap).
func drainStream(w io.Writer, lp string, off int64) int64 {
	fi, err := os.Stat(lp)
	if err != nil {
		return off
	}
	if fi.Size() < off {
		off = 0
	}
	if fi.Size() == off {
		return off
	}
	f, err := os.Open(lp)
	if err != nil {
		return off
	}
	defer f.Close()
	if _, err := f.Seek(off, 0); err != nil {
		return off
	}
	b, _ := io.ReadAll(f)
	lines, consumed := completeLines(b)
	for _, line := range lines {
		io.WriteString(w, "event: stream\ndata: "+line+"\n\n")
	}
	return off + int64(consumed)
}

// completeLines returns the non-empty lines fully terminated by a newline in b,
// plus the byte count consumed (the offset just past the last '\n'). A trailing
// partial line (no newline yet) is left unconsumed so a later read emits it whole
// — agent output is never split mid-line across SSE frames.
func completeLines(b []byte) (lines []string, consumed int) {
	nl := bytes.LastIndexByte(b, '\n')
	if nl < 0 {
		return nil, 0
	}
	return splitNonEmptyLines(string(b[:nl+1])), nl + 1
}
