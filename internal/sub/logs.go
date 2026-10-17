package sub

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

// LogOptions controls the initial snapshot and subsequent streaming. Tail -1
// selects all lines; Tail 0 suppresses the snapshot but still follows new lines.
type LogOptions struct {
	Tail   int
	Follow bool
	Raw    bool
	// Structured emits safe JSON projections, never raw harness payloads.
	Structured bool
	// ReadOnly uses atomic state snapshots without recovering orphan workers.
	ReadOnly bool
}

func logLine(line string) string {
	event, ok := parseLogEvent(line)
	if !ok {
		return "stderr: " + singleLogLine(line)
	}
	return formatLogEvent(event)
}

func formatLogEvent(event logEvent) string {
	parts := []string{}
	stamp := event.Time
	if stamp == "" {
		stamp = event.Timestamp
	}
	if stamp != "" {
		parts = append(parts, singleLogLine(stamp))
	}
	parts = append(parts, singleLogLine(string(event.Kind)))
	text := event.Text
	switch event.Kind {
	case cockpit.EventTool:
		if event.Tool != nil {
			parts = append(parts, singleLogLine(event.Tool.Name))
			text = event.Tool.Text
		}
	case cockpit.EventState, cockpit.EventSystem:
		if event.State != "" {
			parts = append(parts, singleLogLine(event.State))
		}
	case cockpit.EventError:
		if text == "" {
			text = event.Err
		}
	case cockpit.EventUsage:
		if event.Usage != nil {
			text = fmt.Sprintf("in %d / out %d", event.Usage.InputTokens, event.Usage.OutputTokens)
		}
	}
	if text != "" {
		parts = append(parts, singleLogLine(text))
	}
	return strings.Join(parts, " ")
}

func singleLogLine(text string) string {
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = text[:i]
	}
	text = strings.ReplaceAll(text, "\t", " ")
	runes := []rune(text)
	if len(runes) > 240 {
		text = string(runes[:240]) + "…"
	}
	return text
}

// logReader retains partial lines across EOF. A growing log is not a stream
// whose EOF terminates a record; only newline commits a line while following.
type logReader struct {
	path      string
	file      *os.File
	reader    *bufio.Reader
	pending   []byte
	window    int64
	unlimited bool
	discard   bool
	truncated bool
}

func (r *logReader) close() {
	if r.file != nil {
		_ = r.file.Close()
	}
}
func (r *logReader) drain(emit func(string) error) error {
	if r.file == nil {
		file, err := os.Open(r.path)
		// Workers create the log after claiming the job; early failures have no log.
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		r.file, r.reader = file, bufio.NewReader(file)
		if r.window > 0 {
			info, err := file.Stat()
			if err != nil {
				return err
			}
			if info.Size() > r.window {
				if _, err := file.Seek(info.Size()-r.window-1, io.SeekStart); err != nil {
					return err
				}
				// Read the byte before the window to distinguish a record boundary from
				// a partial first record. Never render a fragment as a complete event.
				b, err := r.reader.ReadByte()
				if err != nil {
					return err
				}
				r.discard = b != '\n'
			}
		}
	}
	for {
		chunk, err := r.reader.ReadSlice('\n')
		complete := len(chunk) > 0 && chunk[len(chunk)-1] == '\n'
		if complete {
			chunk = chunk[:len(chunk)-1]
		}
		if !r.discard && len(chunk) > 0 {
			const maxLine = 64 << 10
			if !r.unlimited && len(chunk) > maxLine-len(r.pending) {
				chunk = chunk[:maxLine-len(r.pending)]
				r.truncated = true
			}
			r.pending = append(r.pending, chunk...)
		}
		if complete {
			if !r.discard {
				line := string(r.pending)
				if r.truncated {
					line += "… [line truncated]"
				}
				if err := emit(line); err != nil {
					return err
				}
			}
			r.pending = r.pending[:0]
			r.discard, r.truncated = false, false
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// Logs normally recovers orphan jobs through the per-job lifecycle lock, without
// holding it while writing or waiting. ReadOnly uses atomic Load snapshots instead.
func (s Store) Logs(ctx context.Context, id string, out io.Writer, options LogOptions) error {
	if options.Tail < -1 {
		return fmt.Errorf("tail must be -1 or nonnegative")
	}
	load := s.Recover
	if options.ReadOnly {
		load = s.Load
	}
	initial, err := load(id)
	if err != nil {
		return err
	}
	reader := &logReader{path: s.LogPath(id), unlimited: !options.Follow}
	if options.Follow && options.Tail >= 0 {
		reader.window = 1 << 20
	}
	defer reader.close()
	emit := func(line string) error { _, err := fmt.Fprintln(out, line); return err }
	renderer := &logRenderer{raw: options.Raw, structured: options.Structured}
	var tail []string
	next := 0
	snapshot := emit
	if options.Tail >= 0 {
		snapshot = func(line string) error {
			if options.Tail == 0 {
				return nil
			}
			if len(tail) < options.Tail {
				tail = append(tail, line)
			} else {
				tail[next] = line
			}
			next = (next + 1) % options.Tail
			return nil
		}
	}
	acceptSnapshot := func(line string) error { return renderer.add(line, snapshot) }
	if err := reader.drain(acceptSnapshot); err != nil {
		return err
	}
	if (!options.Follow || Terminal(initial.Status)) && len(reader.pending) > 0 {
		if err := acceptSnapshot(string(reader.pending)); err != nil {
			return err
		}
		reader.pending = reader.pending[:0]
	}
	if !options.Follow || Terminal(initial.Status) {
		if err := renderer.flush(snapshot); err != nil {
			return err
		}
	}
	for i := 0; i < len(tail); i++ {
		if err := emit(tail[(next+i)%len(tail)]); err != nil {
			return err
		}
	}
	if !options.Follow {
		return nil
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		st, err := load(id)
		if err != nil {
			return err
		}
		// Always drain after observing terminal state: the writer may have appended
		// its last events between our previous EOF and publishing that state.
		accept := func(line string) error { return renderer.add(line, emit) }
		if err := reader.drain(accept); err != nil {
			return err
		}
		if Terminal(st.Status) {
			// Terminal state is published after the writer closes; an unterminated
			// stderr fragment is now final, rather than a partially written event.
			if len(reader.pending) > 0 {
				if err := accept(string(reader.pending)); err != nil {
					return err
				}
			}
			return renderer.flush(emit)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
