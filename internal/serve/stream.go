package serve

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strconv"
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

	// A reconnecting EventSource replays Last-Event-ID: the 1-based ordinal of the
	// last line it received. Resuming past it (instead of re-sending the tail-200)
	// keeps reconnects from duplicating backfill in the terminal.
	lastID := -1
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			lastID = n
		}
	}

	// Backfill from a single read so the resume offset is the exact byte boundary
	// of the bytes we delivered — never a separate Stat that could race an append
	// in between and re-deliver those bytes on the first tick. off lands on the
	// last newline; any trailing partial line waits for drainStream to emit it whole.
	var off int64
	var ord int
	if data, err := os.ReadFile(lp); err == nil {
		lines, consumed := completeLines(data)
		start := 0
		switch {
		case lastID >= 0:
			start = min(lastID, len(lines))
		case len(lines) > eventBackfill:
			start = len(lines) - eventBackfill
		}
		for i := start; i < len(lines); i++ {
			writeStreamFrame(w, i+1, lines[i])
		}
		off = int64(consumed)
		ord = len(lines)
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
			off, ord = drainStreamFrom(w, lp, off, ord)
			fl.Flush()
		}
	}
}

// drainStream writes any bytes appended past off as SSE frames and returns the
// new offset. Re-reads from 0 if the file shrank (truncation/branch swap).
func drainStream(w io.Writer, lp string, off int64) int64 {
	off, _ = drainStreamFrom(w, lp, off, 0)
	return off
}

// drainStreamFrom is drainStream carrying ord, the 1-based ordinal of the last
// line emitted, so live frames get SSE ids that continue the backfill's sequence.
// Ordinals restart with the byte offset when the file shrank.
func drainStreamFrom(w io.Writer, lp string, off int64, ord int) (int64, int) {
	fi, err := os.Stat(lp)
	if err != nil {
		return off, ord
	}
	if fi.Size() < off {
		off, ord = 0, 0
	}
	if fi.Size() == off {
		return off, ord
	}
	f, err := os.Open(lp)
	if err != nil {
		return off, ord
	}
	defer f.Close()
	if _, err := f.Seek(off, 0); err != nil {
		return off, ord
	}
	b, _ := io.ReadAll(f)
	lines, consumed := completeLines(b)
	for _, line := range lines {
		ord++
		writeStreamFrame(w, ord, line)
	}
	return off + int64(consumed), ord
}

// writeStreamFrame emits one agent-output line as an SSE frame whose id is the
// line's 1-based ordinal in the stream file — the reconnect resume cursor.
func writeStreamFrame(w io.Writer, ord int, line string) {
	io.WriteString(w, "id: "+strconv.Itoa(ord)+"\nevent: stream\ndata: "+line+"\n\n")
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
