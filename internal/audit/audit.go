// Package audit is Pactify's local-first permission audit log: it records every
// tool call an agent makes (Bash/file/MCP) to a machine-local append-only JSONL
// store, attributed to the seat/task/project that produced it. Log-only — it
// never blocks an agent (governance is a deferred follow-up).
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
