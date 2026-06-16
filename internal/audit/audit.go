// Package audit is Pactify's local-first permission audit log: it records every
// tool call an agent makes (Bash/file/MCP) to a machine-local append-only JSONL
// store, attributed to the seat/task/project that produced it. Log-only — it
// never blocks an agent (governance is a deferred follow-up).
package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Record is one captured tool call. Forward-compatible: readers ignore unknown
// fields. `Decision` is present from v1 (always "allow") so the schema is stable
// when governance lands.
type Record struct {
	TS       string `json:"ts"`
	Project  string `json:"project"`
	Repo     string `json:"repo"`
	Seat     string `json:"seat"`
	Task     string `json:"task"`
	Kind     string `json:"kind"`
	Session  string `json:"session"`
	Tool     string `json:"tool"`
	Summary  string `json:"summary"`
	Risk     string `json:"risk"`
	Decision string `json:"decision"`
}

// home resolves the Pactify home dir: PACTIFY_HOME override (tests) else ~/.pactify.
// Mirrors internal/registry's convention so CLI/serve/audit agree.
func home() (string, error) {
	if h := os.Getenv("PACTIFY_HOME"); h != "" {
		return filepath.Join(h, ".pactify"), nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".pactify"), nil
}

// dayOf extracts the UTC date (YYYY-MM-DD) from an RFC3339 ts; "" → "unknown".
func dayOf(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return "unknown"
}

// storePath is ~/.pactify/audit/<project>/<YYYY-MM-DD>.jsonl. A missing project
// buckets under "_unknown" so a record is never dropped.
func storePath(project, ts string) (string, error) {
	h, err := home()
	if err != nil {
		return "", err
	}
	p := project
	if p == "" {
		p = "_unknown"
	}
	return filepath.Join(h, "audit", p, dayOf(ts)+".jsonl"), nil
}

// Append writes one record as a JSON line (O_APPEND). Best-effort: it returns an
// error for the caller to log, never panics.
func Append(r Record) error {
	path, err := storePath(r.Project, r.TS)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("audit append: %w", err)
	}
	return nil
}

var _ = strings.TrimSpace

// Filter selects records on read. "" / zero = match any.
type Filter struct {
	Project, Seat, Task, Session, Risk string
	Since, Until                       time.Time
}

func (f Filter) match(r Record) bool {
	if f.Seat != "" && r.Seat != f.Seat {
		return false
	}
	if f.Task != "" && r.Task != f.Task {
		return false
	}
	if f.Session != "" && r.Session != f.Session {
		return false
	}
	if f.Risk != "" && r.Risk != f.Risk {
		return false
	}
	if !f.Since.IsZero() || !f.Until.IsZero() {
		ts, err := time.Parse(time.RFC3339, r.TS)
		if err != nil {
			return false
		}
		if !f.Since.IsZero() && ts.Before(f.Since) {
			return false
		}
		if !f.Until.IsZero() && ts.After(f.Until) {
			return false
		}
	}
	return true
}

// Query folds the project's day-files, returns matches newest-first, and skips
// unparseable (torn) lines rather than failing the whole read.
func Query(f Filter) ([]Record, error) {
	h, err := home()
	if err != nil {
		return nil, err
	}
	proj := f.Project
	if proj == "" {
		proj = "_unknown"
	}
	dir := filepath.Join(h, "audit", proj)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no audit yet → empty, not an error
		}
		return nil, err
	}
	var out []Record
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		file, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(file)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			var r Record
			if json.Unmarshal(sc.Bytes(), &r) != nil {
				continue // torn/garbage line
			}
			if f.match(r) {
				out = append(out, r)
			}
		}
		file.Close()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS > out[j].TS }) // newest-first
	return out, nil
}

// Summary aggregates counts for a digest.
type Summary struct {
	Total  int
	ByTool map[string]int
	ByRisk map[string]int
	BySeat map[string]int
}

// Summarize counts records by tool/risk/seat.
func Summarize(rs []Record) Summary {
	s := Summary{ByTool: map[string]int{}, ByRisk: map[string]int{}, BySeat: map[string]int{}}
	for _, r := range rs {
		s.Total++
		s.ByTool[r.Tool]++
		s.ByRisk[r.Risk]++
		s.BySeat[r.Seat]++
	}
	return s
}
