package event

import (
	"bufio"
	"encoding/json"
	"os"
)

// Append writes one event as a single JSON line (atomic under O_APPEND).
func Append(logPath string, ev Event) error {
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
		return err
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// ReadAll parses every event line. Unknown top-level fields and unknown
// event_types are tolerated. A missing file yields nil, nil.
func ReadAll(logPath string) ([]Event, error) {
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var evs []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		b := sc.Bytes()
		if len(b) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(b, &ev); err != nil {
			return nil, err
		}
		evs = append(evs, ev)
	}
	return evs, sc.Err()
}

// writeFile is a small test/helper for writing a whole file.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
