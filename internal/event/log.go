package event

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
)

// Append writes one event as a single JSON line (atomic under O_APPEND).
// Append writes one event and returns nothing; callers that need the exact
// bytes written should use AppendLine.
func Append(logPath string, ev Event) error {
	_, err := AppendLine(logPath, ev)
	return err
}

// AppendLine appends the event and returns the EXACT line written.
//
// The defaults below (event_id, ts, payload) are filled in HERE, not by the
// caller — so a caller that marshals its own copy of `ev` produces a different
// line than the one on disk. The ledger-ref mirror needs byte-identical content
// or its drift check reports a difference on every single event.
func AppendLine(logPath string, ev Event) (string, error) {
	if ev.EventID == "" {
		ev.EventID = NewEventID()
	}
	if ev.TS == "" {
		ev.TS = NowUTC()
	}
	if ev.Payload == nil {
		ev.Payload = map[string]any{}
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return "", err
	}
	return string(line), nil
}

// ParseStats reports what happened during a ledger read.
type ParseStats struct {
	Skipped int // lines skipped because they were malformed or oversized
}

// ReadAll parses every event line. Unknown top-level fields and unknown
// event_types are tolerated. A missing file yields nil, nil.
func ReadAll(logPath string) ([]Event, error) {
	evs, _, err := ReadAllWithStats(logPath)
	return evs, err
}

// ReadAllWithStats is like ReadAll but also returns per-line parse statistics.
// Callers that must distinguish "empty/absent log" from "log exists but is
// unreadable" can check stats.Skipped.
func ReadAllWithStats(logPath string) ([]Event, ParseStats, error) {
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ParseStats{}, nil
		}
		return nil, ParseStats{}, err
	}
	defer f.Close()
	return parseWithStats(f)
}

// ParseAll parses events from an in-memory log slice, byte-for-byte equivalent to
// ReadAll over a file holding the same bytes. The snapshot fold uses it to parse a
// tail slice (the bytes after the folded offset) without re-reading the file. data
// must start on a line boundary — the ledger is append-only with a trailing newline
// per event, so the snapshot offset always lands on one.
func ParseAll(data []byte) ([]Event, error) {
	evs, _, err := parseWithStats(bytes.NewReader(data))
	return evs, err
}

func parse(r io.Reader) ([]Event, error) {
	evs, _, err := parseWithStats(r)
	return evs, err
}

func parseWithStats(r io.Reader) ([]Event, ParseStats, error) {
	var evs []Event
	var stats ParseStats
	br := bufio.NewReaderSize(r, 16<<20)
	const maxLine = 16 << 20

	for {
		line, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// Line exceeds the 16 MiB buffer. Drain the rest of the line
			// without buffering it unboundedly, then skip it.
			stats.Skipped++
			for err == bufio.ErrBufferFull {
				_, err = br.ReadSlice('\n')
			}
			if err == io.EOF {
				break
			}
			continue
		}
		if err != nil && err != io.EOF {
			return evs, stats, err
		}

		if len(line) > 0 {
			// Strip trailing newline (and any CR from CRLF) to match scanner semantics.
			if line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if len(line) == 0 {
				// empty line
			} else if len(line) <= maxLine {
				var ev Event
				if err := json.Unmarshal(line, &ev); err != nil {
					stats.Skipped++
					continue
				}
				evs = append(evs, ev)
			}
		}

		if err == io.EOF {
			break
		}
	}
	return evs, stats, nil
}

// writeFile is a small test/helper for writing a whole file.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
