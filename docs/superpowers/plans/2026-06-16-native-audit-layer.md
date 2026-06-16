# Native Audit Layer — Implementation Plan

> **For agentic workers:** This plan is a **pact task graph** driven by
> `pactify orchestrate` (claude = orchestrator/reviewer + complex sticks;
> opencode = standard worker sticks; reviewer flips per task so owner ≠ reviewer).
> Each Task below maps to one pact task with an owner, a reviewer, deps, and a
> `verify:` command (the hard gate). Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add a Pactify-native permission **audit log** — capture every tool call
each seat makes while executing a task, append it to a machine-local JSONL store,
and surface it via CLI + a dashboard lens.

**Architecture:** A `pactify audit hook` PreToolUse subcommand (installed into each
client's settings) reads the tool-call JSON on stdin, stamps seat/task/project from
env, normalizes it, and appends one line to `~/.pactify/audit/<project>/<date>.jsonl`.
The orchestrate runner stamps `PACT_TASK_ID`/`PACT_PROJECT` (via a new
`LaunchContext`). CLI + a serve endpoint + a React lens read it back. Log-only v1
(always allow); governance is a deferred P2 on the same seam.

**Tech Stack:** Go (`internal/audit`, cobra `audit` subcommands, `internal/serve`),
append-only JSONL, React + Tailwind v4 dashboard. No Rust/DB/daemon.

**Spec:** `docs/superpowers/specs/2026-06-16-native-audit-layer-design.md`

---

## File Structure

**New:**
- `internal/audit/audit.go` — `Record`, `Filter`, `Summary`, store path, `Append`, `Query`, `Summarize`.
- `internal/audit/audit_test.go`
- `internal/audit/hook.go` — `Env`, `EnvFromOS`, `FromHook` (per-kind parse), `redact`, `mapTool`.
- `internal/audit/hook_test.go`
- `internal/audit/install.go` — `Install`, `Uninstall`, `Detect` (claude-code + opencode settings upsert).
- `internal/audit/install_test.go`
- `cmd/pactify/cmd_audit.go` — `audit` command group (`hook`/`log`/`summary`/`prune`/`install`/`uninstall`/`detect`).
- `cmd/pactify/cmd_audit_test.go`
- `internal/serve/audit.go` — `GET /api/projects/{id}/audit`.
- `internal/serve/audit_test.go`
- `web/src/components/AuditLens.tsx` — the dashboard Audit lens.
- `web/src/components/AuditLens.test.tsx`

**Modified:**
- `internal/orchestrate/runner.go` — `LaunchContext`, `Runner.Run` signature, env stamping.
- `internal/orchestrate/loop.go` — `launchAgent` + `runOwner`/`runReviewer` thread task/project.
- `internal/orchestrate/loop_test.go`, `parallel_test.go` — fake runner signatures.
- `cmd/pactify/commands.go:125` — register `newAuditCmd()`.
- `web/src/lib/api.ts` — `getAudit`.
- `internal/serve/api.go` (or wherever routes register) — wire `registerAuditRoutes`.

---

## Pact Task Graph (orchestrate drives this)

| Task | Feature | Owner | Reviewer | Deps | verify: |
|------|---------|-------|----------|------|---------|
| T1 store | audit-core | **opencode** | claude | — | `go test ./internal/audit/` |
| T2 query | audit-core | **opencode** | claude | T1 | `go test ./internal/audit/` |
| T3 fromhook | audit-capture | **claude** | opencode | T1 | `go test ./internal/audit/` |
| T4 hook-cmd | audit-capture | **claude** | opencode | T3 | `go test ./cmd/pactify/ -run Audit` |
| T5 launchctx | runner | **claude** | opencode | — | `go test ./internal/orchestrate/` |
| T6 cli | audit-cli | **opencode** | claude | T2 | `go test ./cmd/pactify/ -run Audit` |
| T7 install-claude | wiring | **claude** | opencode | T4 | `go test ./internal/audit/ -run Install` |
| T8 install-opencode | wiring | **claude** | opencode | T7 | `go test ./internal/audit/ -run Install` |
| T9 serve | dashboard | **claude** | opencode | T2 | `go test ./internal/serve/ -run Audit` |
| T10 lens | dashboard | **claude** | opencode | T9 | `cd web && npm test -- AuditLens` |

Orchestrate runs serial; deps gate order. Owner ≠ reviewer everywhere (engine rule).

> **Side observation for this run:** opencode is the worker on T1/T2/T6. After each
> of those tasks is **accepted**, confirm `opencode session list` no longer shows a
> `pact:<opencode-seat>` session (the 2026-06-15 cleanup, commit 55b829a). Record
> the before/after in the run notes.

---

## Task 1: Audit store (Record + Append) — owner opencode

**Files:**
- Create: `internal/audit/audit.go`
- Test: `internal/audit/audit_test.go`

- [ ] **Step 1: Write the failing test**

```go
package audit

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestAppendAndStorePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	r := Record{
		TS: "2026-06-16T05:10:00Z", Project: "demo", Repo: "/x", Seat: "dev",
		Task: "t1", Kind: "opencode", Session: "ses_1", Tool: "bash",
		Summary: "go test ./...", Risk: "exec", Decision: "allow",
	}
	if err := Append(r); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// File at ~/.pactify/audit/demo/2026-06-16.jsonl, one JSON line.
	want := home + "/.pactify/audit/demo/2026-06-16.jsonl"
	b, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	if !strings.Contains(string(b), `"tool":"bash"`) || !strings.HasSuffix(string(b), "\n") {
		t.Fatalf("store line = %q", b)
	}
}

func TestAppendDateBucketsByUTC(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	mustAppend(t, Record{Project: "p", TS: "2026-06-16T23:59:00Z", Tool: "a"})
	mustAppend(t, Record{Project: "p", TS: "2026-06-17T00:01:00Z", Tool: "b"})
	for _, d := range []string{"2026-06-16", "2026-06-17"} {
		if _, err := os.Stat(home + "/.pactify/audit/p/" + d + ".jsonl"); err != nil {
			t.Errorf("missing day file %s: %v", d, err)
		}
	}
}

func mustAppend(t *testing.T, r Record) {
	t.Helper()
	if err := Append(r); err != nil {
		t.Fatal(err)
	}
}

var _ = time.Now
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/audit/`
Expected: FAIL — `undefined: Record` / `undefined: Append`.

- [ ] **Step 3: Write minimal implementation**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/audit/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/audit/audit.go internal/audit/audit_test.go
git commit -m "feat(audit): Record + append-only JSONL store (~/.pactify/audit)"
```

---

## Task 2: Query + Summarize — owner opencode (deps: T1)

**Files:**
- Modify: `internal/audit/audit.go`
- Test: `internal/audit/audit_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestQueryFiltersAndOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	mustAppend(t, Record{Project: "p", TS: "2026-06-16T01:00:00Z", Seat: "dev", Task: "t1", Tool: "bash", Risk: "exec"})
	mustAppend(t, Record{Project: "p", TS: "2026-06-16T02:00:00Z", Seat: "rev", Task: "t1", Tool: "fs.read", Risk: "read"})
	mustAppend(t, Record{Project: "p", TS: "2026-06-16T03:00:00Z", Seat: "dev", Task: "t2", Tool: "fs.write", Risk: "write"})

	// Filter by seat=dev → 2 records, newest-first.
	got, err := Query(Filter{Project: "p", Seat: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Task != "t2" || got[1].Task != "t1" {
		t.Fatalf("seat filter = %+v", got)
	}
	// Filter by task=t1 → 2 records.
	if got, _ := Query(Filter{Project: "p", Task: "t1"}); len(got) != 2 {
		t.Fatalf("task filter len = %d, want 2", len(got))
	}
	// Filter by risk=write → 1.
	if got, _ := Query(Filter{Project: "p", Risk: "write"}); len(got) != 1 {
		t.Fatalf("risk filter len = %d, want 1", len(got))
	}
}

func TestQuerySkipsTornLines(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	mustAppend(t, Record{Project: "p", TS: "2026-06-16T01:00:00Z", Tool: "bash"})
	// Append a torn/garbage line directly.
	p := home + "/.pactify/audit/p/2026-06-16.jsonl"
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("{not json\n")
	f.Close()
	got, err := Query(Filter{Project: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 valid record (torn line skipped), got %d", len(got))
	}
}

func TestSummarize(t *testing.T) {
	rs := []Record{
		{Tool: "bash", Risk: "exec", Seat: "dev"},
		{Tool: "bash", Risk: "exec", Seat: "dev"},
		{Tool: "fs.write", Risk: "write", Seat: "rev"},
	}
	s := Summarize(rs)
	if s.Total != 3 || s.ByRisk["exec"] != 2 || s.BySeat["dev"] != 2 || s.ByTool["bash"] != 2 {
		t.Fatalf("summary = %+v", s)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/audit/`
Expected: FAIL — `undefined: Query` / `undefined: Filter` / `undefined: Summarize`.

- [ ] **Step 3: Write minimal implementation** (append to `audit.go`)

```go
import (
	"bufio"
	"sort"
	"time"
)

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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/audit/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/audit/audit.go internal/audit/audit_test.go
git commit -m "feat(audit): Query(Filter) + Summarize (newest-first, torn-line tolerant)"
```

---

## Task 3: FromHook (per-kind parse + redaction) — owner claude (deps: T1)

**Files:**
- Create: `internal/audit/hook.go`
- Test: `internal/audit/hook_test.go`

- [ ] **Step 1: Write the failing test**

```go
package audit

import (
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 6, 16, 5, 10, 0, 0, time.UTC)

func TestFromHookClaudeBash(t *testing.T) {
	stdin := []byte(`{"tool_name":"Bash","tool_input":{"command":"git push origin main"},"session_id":"ses_x","cwd":"/repo/demo"}`)
	env := Env{Seat: "dev", Task: "t3", Project: "demo"}
	r, ok := FromHook("claude-code", stdin, env, fixedTime)
	if !ok {
		t.Fatal("expected ok=true for Bash")
	}
	if r.Tool != "bash" || r.Risk != "exec" || r.Summary != "git push origin main" {
		t.Fatalf("record = %+v", r)
	}
	if r.Seat != "dev" || r.Task != "t3" || r.Project != "demo" || r.Kind != "claude-code" {
		t.Fatalf("attribution = %+v", r)
	}
	if r.Session != "ses_x" || r.Repo != "/repo/demo" || r.Decision != "allow" {
		t.Fatalf("fields = %+v", r)
	}
	if r.TS != "2026-06-16T05:10:00Z" {
		t.Fatalf("ts = %q", r.TS)
	}
}

func TestFromHookClaudeFileTools(t *testing.T) {
	w := []byte(`{"tool_name":"Write","tool_input":{"file_path":"/repo/x.go"},"cwd":"/repo"}`)
	if r, _ := FromHook("claude-code", w, Env{}, fixedTime); r.Tool != "fs.write" || r.Risk != "write" || r.Summary != "/repo/x.go" {
		t.Fatalf("write = %+v", r)
	}
	rd := []byte(`{"tool_name":"Read","tool_input":{"file_path":"/repo/y.go"},"cwd":"/repo"}`)
	if r, _ := FromHook("claude-code", rd, Env{}, fixedTime); r.Tool != "fs.read" || r.Risk != "read" {
		t.Fatalf("read = %+v", r)
	}
}

func TestFromHookMCPPassthrough(t *testing.T) {
	m := []byte(`{"tool_name":"mcp__pact__status","tool_input":{"project":"demo"},"cwd":"/repo"}`)
	r, ok := FromHook("claude-code", m, Env{}, fixedTime)
	if !ok || r.Tool != "mcp__pact__status" || r.Risk != "mcp" {
		t.Fatalf("mcp = %+v ok=%v", r, ok)
	}
}

func TestFromHookUnmappedReturnsFalse(t *testing.T) {
	if _, ok := FromHook("claude-code", []byte(`{"tool_name":"TodoWrite","tool_input":{}}`), Env{}, fixedTime); ok {
		t.Fatal("unmapped tool should return ok=false")
	}
	if _, ok := FromHook("claude-code", []byte(`{not json`), Env{}, fixedTime); ok {
		t.Fatal("malformed stdin should return ok=false")
	}
}

func TestRedactionTruncatesAndMasksSecrets(t *testing.T) {
	long := "echo " + makeLong(400)
	in := []byte(`{"tool_name":"Bash","tool_input":{"command":"` + long + `"},"cwd":"/r"}`)
	r, _ := FromHook("claude-code", in, Env{}, fixedTime)
	if len(r.Summary) > 210 {
		t.Fatalf("summary not truncated: %d chars", len(r.Summary))
	}
	sec := []byte(`{"tool_name":"Bash","tool_input":{"command":"curl -H 'Authorization: Bearer sk-abc123secret' x"},"cwd":"/r"}`)
	r2, _ := FromHook("claude-code", sec, Env{}, fixedTime)
	if contains(r2.Summary, "sk-abc123secret") {
		t.Fatalf("secret not masked: %q", r2.Summary)
	}
}

func makeLong(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/audit/ -run FromHook`
Expected: FAIL — `undefined: FromHook` / `undefined: Env`.

- [ ] **Step 3: Write minimal implementation**

```go
package audit

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"time"
)

// Env is the orchestrate context the runner stamps into the agent's environment;
// the hook reads it to attribute a tool call to a seat/task/project. Empty fields
// (a human running the agent directly) are fine — the record still lands.
type Env struct{ Seat, Task, Project string }

// EnvFromOS reads PACT_AGENT_ID / PACT_TASK_ID / PACT_PROJECT from the process env.
func EnvFromOS() Env {
	return Env{
		Seat:    os.Getenv("PACT_AGENT_ID"),
		Task:    os.Getenv("PACT_TASK_ID"),
		Project: os.Getenv("PACT_PROJECT"),
	}
}

const summaryCap = 200

type hookInput struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	SessionID string          `json:"session_id"`
	Cwd       string          `json:"cwd"`
}

// FromHook parses a client's PreToolUse stdin into a Record, stamping env +
// session/cwd. ok=false when the tool is unmapped or the input is malformed (the
// caller then no-ops, exit 0). Pure: (kind, bytes, env, now) → Record.
func FromHook(kind string, stdin []byte, env Env, now time.Time) (Record, bool) {
	var in hookInput
	if json.Unmarshal(stdin, &in) != nil || in.ToolName == "" {
		return Record{}, false
	}
	tool, summary, risk, ok := mapTool(kind, in.ToolName, in.ToolInput)
	if !ok {
		return Record{}, false
	}
	project := env.Project
	if project == "" && in.Cwd != "" {
		project = baseName(in.Cwd)
	}
	return Record{
		TS:       now.UTC().Format(time.RFC3339),
		Project:  project,
		Repo:     in.Cwd,
		Seat:     env.Seat,
		Task:     env.Task,
		Kind:     kind,
		Session:  in.SessionID,
		Tool:     tool,
		Summary:  redact(summary),
		Risk:     risk,
		Decision: "allow",
	}, true
}

// mapTool normalizes a client's tool vocabulary to the canonical set. claude-code
// and opencode share Claude's PreToolUse shape (Bash/Read/Write/Edit + mcp__*);
// per-kind field differences (if any) are handled here. opencode's exact field
// names are a verification item — adjust this branch after 实测 (Task 8 notes).
func mapTool(kind, name string, rawInput json.RawMessage) (tool, summary, risk string, ok bool) {
	var fields struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
	}
	_ = json.Unmarshal(rawInput, &fields)
	path := fields.FilePath
	if path == "" {
		path = fields.Path
	}
	switch name {
	case "Bash":
		return "bash", fields.Command, "exec", true
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		return "fs.write", path, "write", true
	case "Read":
		return "fs.read", path, "read", true
	default:
		if strings.HasPrefix(name, "mcp__") {
			return name, name, "mcp", true
		}
		return "", "", "", false
	}
}

func baseName(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

var secretRe = regexp.MustCompile(`(?i)(bearer\s+|token[=:]\s*|secret[=:]\s*|sk-)[A-Za-z0-9._\-]+`)

// redact masks secret-ish runs and truncates to summaryCap chars.
func redact(s string) string {
	s = secretRe.ReplaceAllString(s, "$1***")
	if len(s) > summaryCap {
		s = s[:summaryCap] + "…"
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/audit/`
Expected: PASS (all audit tests).

- [ ] **Step 5: Commit**

```bash
git add internal/audit/hook.go internal/audit/hook_test.go
git commit -m "feat(audit): FromHook per-kind tool normalization + secret redaction"
```

---

## Task 4: `pactify audit hook` command — owner claude (deps: T3)

**Files:**
- Create: `cmd/pactify/cmd_audit.go`
- Modify: `cmd/pactify/commands.go:125` (register `newAuditCmd()`)
- Test: `cmd/pactify/cmd_audit_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"bytes"
	"os"
	"testing"
)

func TestAuditHookAppendsAndExitsZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PACT_AGENT_ID", "dev")
	t.Setenv("PACT_TASK_ID", "t1")
	t.Setenv("PACT_PROJECT", "demo")

	cmd := newAuditCmd()
	cmd.SetArgs([]string{"hook", "--kind", "claude-code"})
	cmd.SetIn(bytes.NewBufferString(`{"tool_name":"Bash","tool_input":{"command":"go build"},"cwd":"/r","session_id":"s1"}`))
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil { // hook must never error (never block the agent)
		t.Fatalf("hook returned error: %v", err)
	}
	if _, err := os.Stat(home + "/.pactify/audit/demo/" + todayUTC() + ".jsonl"); err != nil {
		t.Fatalf("audit not written: %v", err)
	}
}

func TestAuditHookBadInputStillExitsZero(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	cmd := newAuditCmd()
	cmd.SetArgs([]string{"hook", "--kind", "claude-code"})
	cmd.SetIn(bytes.NewBufferString(`{garbage`))
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("hook on bad input must not error: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/pactify/ -run Audit`
Expected: FAIL — `undefined: newAuditCmd` / `undefined: todayUTC`.

- [ ] **Step 3: Write minimal implementation** (`cmd_audit.go`; model on `cmd_sessions.go`)

```go
package main

import (
	"io"
	"time"

	"github.com/agentjoey/pactify/internal/audit"
	"github.com/spf13/cobra"
)

func todayUTC() string { return time.Now().UTC().Format("2006-01-02") }

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "permission audit log — capture, query, and manage per-seat tool-call records",
	}
	cmd.AddCommand(newAuditHookCmd())
	return cmd
}

func newAuditHookCmd() *cobra.Command {
	var kind string
	c := &cobra.Command{
		Use:    "hook --kind <kind>",
		Short:  "PreToolUse hook entry: read a tool call on stdin, record it, allow (exit 0)",
		Hidden: true, // wired by clients, not run by humans
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Best-effort: ANY failure must still exit 0 so the agent is never blocked.
			stdin, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return nil
			}
			rec, ok := audit.FromHook(kind, stdin, audit.EnvFromOS(), time.Now())
			if !ok {
				return nil
			}
			_ = audit.Append(rec) // swallow store errors — never block the agent
			return nil
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "client kind firing the hook (claude-code | opencode)")
	return c
}
```

Then register in `cmd/pactify/commands.go` (the `root.AddCommand(...)` at ~line 125): add `newAuditCmd()` to the argument list.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/pactify/ -run Audit`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/pactify/cmd_audit.go cmd/pactify/cmd_audit_test.go cmd/pactify/commands.go
git commit -m "feat(audit): pactify audit hook (stdin→record→allow, never blocks agent)"
```

---

## Task 5: LaunchContext + runner env stamping — owner claude (deps: none)

**Files:**
- Modify: `internal/orchestrate/runner.go`, `internal/orchestrate/loop.go`
- Modify tests: `internal/orchestrate/loop_test.go`, `parallel_test.go`
- Test: `internal/orchestrate/runner_test.go`

- [ ] **Step 1: Write the failing test** (append to `runner_test.go`)

```go
func TestRunnerStampsTaskAndProjectEnv(t *testing.T) {
	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, nil)}
	lc := LaunchContext{Seat: "dev", Kind: "opencode", Task: "t7", Project: "demo", Briefing: "B", RepoDir: "/repo"}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !hasEnv(cap.env, "PACT_AGENT_ID=dev") || !hasEnv(cap.env, "PACT_TASK_ID=t7") || !hasEnv(cap.env, "PACT_PROJECT=demo") {
		t.Fatalf("env missing task/project stamp: %v", cap.env)
	}
}
```

> Note: `runCapture`, `fakeRunExec`, `hasEnv` already exist in `runner_test.go`.
> `cap.env` is the captured `env` slice — confirm the field name; adjust if the
> existing capture names it differently.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrate/ -run RunnerStamps`
Expected: FAIL — `undefined: LaunchContext` / signature mismatch.

- [ ] **Step 3: Write minimal implementation**

In `runner.go` — replace the interface + `CmdRunner.Run` signature:

```go
// LaunchContext carries everything needed to launch one agent stint. Replaces the
// former loose (seatID, kind, briefing, repoDir) params so audit attribution
// (task, project) — and future fields — add without churning the signature.
type LaunchContext struct {
	Seat, Kind, Task, Project, Briefing, RepoDir string
}

type Runner interface {
	Run(ctx context.Context, lc LaunchContext) error
}
```

Update `CmdRunner.Run` to `func (r CmdRunner) Run(ctx context.Context, lc LaunchContext) error`, replacing internal uses of `seatID`→`lc.Seat`, `kind`→`lc.Kind`, `briefing`→`lc.Briefing`, `repoDir`→`lc.RepoDir`. The env block becomes:

```go
	env := []string{
		"PACT_AGENT_ID=" + lc.Seat,
		"PACT_TASK_ID=" + lc.Task,
		"PACT_PROJECT=" + lc.Project,
	}
```

(Keep the existing GLM-env append and the opencode `--title` tagging, which use `lc.Kind`/`lc.Seat`.)

In `loop.go` — `launchAgent` takes a `LaunchContext`:

```go
func (opts Options) launchAgent(ctx context.Context, lc LaunchContext) error {
	runCtx := ctx
	if opts.RunTimeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, opts.RunTimeout)
		defer cancel()
	}
	return opts.Run.Run(runCtx, lc)
}
```

The two callers build the context (project id = base name of the repo dir, matching the audit fallback):

```go
// runOwner:
lc := LaunchContext{Seat: task.Owner, Kind: opts.kind(task.Owner), Task: act.Task,
	Project: projectID(opts.Dir), Briefing: brief, RepoDir: opts.Dir}
if runErr := opts.launchAgent(ctx, lc); runErr != nil { ... }

// runReviewer:
lc := LaunchContext{Seat: task.Reviewer, Kind: opts.kind(task.Reviewer), Task: act.Task,
	Project: projectID(opts.Dir), Briefing: brief, RepoDir: opts.Dir}
if runErr := opts.launchAgent(ctx, lc); runErr != nil { ... }
```

Add a `projectID` helper in `loop.go`:

```go
// projectID derives a stable project name from the repo dir (its base name) — the
// same fallback the audit hook uses when PACT_PROJECT is unset, so they agree.
func projectID(dir string) string { return filepath.Base(dir) }
```

(`path/filepath` is already imported in loop.go.)

Update the 3 test fakes' `Run` signatures to `Run(ctx context.Context, lc LaunchContext) error` and read `lc.Seat`/`lc.Briefing`/`lc.RepoDir` where they asserted the old params:
- `loop_test.go` `fakeRunner.Run`, `crashRunner.Run`
- `parallel_test.go` `parFakeRunner.Run`

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/orchestrate/`
Expected: PASS (all orchestrate tests, including the new stamp test).

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrate/runner.go internal/orchestrate/loop.go internal/orchestrate/runner_test.go internal/orchestrate/loop_test.go internal/orchestrate/parallel_test.go
git commit -m "feat(orchestrate): LaunchContext + stamp PACT_TASK_ID/PACT_PROJECT for audit"
```

---

## Task 6: `audit log` / `summary` / `prune` CLI — owner opencode (deps: T2)

**Files:**
- Modify: `cmd/pactify/cmd_audit.go`
- Test: `cmd/pactify/cmd_audit_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestAuditLogAndSummary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	seed(t, audit.Record{Project: "demo", TS: "2026-06-16T01:00:00Z", Seat: "dev", Task: "t1", Tool: "bash", Summary: "go build", Risk: "exec", Decision: "allow"})
	seed(t, audit.Record{Project: "demo", TS: "2026-06-16T02:00:00Z", Seat: "rev", Task: "t1", Tool: "fs.read", Summary: "/x.go", Risk: "read", Decision: "allow"})

	// log --project demo --json → both records
	out := runAudit(t, "log", "--project", "demo", "--json")
	if !contains(out, `"tool":"bash"`) || !contains(out, `"tool":"fs.read"`) {
		t.Fatalf("log --json missing records: %s", out)
	}
	// summary → total 2
	sum := runAudit(t, "summary", "--project", "demo")
	if !contains(sum, "2") {
		t.Fatalf("summary missing total: %s", sum)
	}
}

func seed(t *testing.T, r audit.Record) { t.Helper(); if err := audit.Append(r); err != nil { t.Fatal(err) } }

func runAudit(t *testing.T, args ...string) string {
	t.Helper()
	cmd := newAuditCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("audit %v: %v", args, err)
	}
	return buf.String()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/pactify/ -run AuditLog`
Expected: FAIL — `log`/`summary` subcommands not defined.

- [ ] **Step 3: Write minimal implementation** (add subcommands; register in `newAuditCmd`)

```go
import (
	"encoding/json"
	"fmt"
	"time"
)

func newAuditLogCmd() *cobra.Command {
	var project, seat, task, session, risk, since string
	var asJSON bool
	var limit int
	c := &cobra.Command{
		Use:   "log",
		Short: "print recent audit records (newest first)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			f := audit.Filter{Project: project, Seat: seat, Task: task, Session: session, Risk: risk}
			if since != "" {
				d, err := time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("--since: %w", err)
				}
				f.Since = time.Now().Add(-d)
			}
			recs, err := audit.Query(f)
			if err != nil {
				return err
			}
			if limit > 0 && len(recs) > limit {
				recs = recs[:limit]
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				for _, r := range recs {
					_ = enc.Encode(r)
				}
				return nil
			}
			for _, r := range recs {
				fmt.Fprintf(out, "%s  %-10s %-8s %-6s %s/%s  %s\n", r.TS, r.Seat, r.Tool, r.Risk, r.Project, r.Task, r.Summary)
			}
			return nil
		},
	}
	c.Flags().StringVar(&project, "project", "", "filter by project")
	c.Flags().StringVar(&seat, "seat", "", "filter by seat")
	c.Flags().StringVar(&task, "task", "", "filter by task")
	c.Flags().StringVar(&session, "session", "", "filter by session id")
	c.Flags().StringVar(&risk, "risk", "", "filter by risk (read|write|exec|mcp)")
	c.Flags().StringVar(&since, "since", "", "only records newer than this (e.g. 24h)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit raw JSON lines")
	c.Flags().IntVar(&limit, "limit", 0, "cap the number of records (0 = all)")
	return c
}

func newAuditSummaryCmd() *cobra.Command {
	var project, since string
	c := &cobra.Command{
		Use:   "summary",
		Short: "counts by tool / risk / seat",
		RunE: func(cmd *cobra.Command, _ []string) error {
			f := audit.Filter{Project: project}
			if since != "" {
				d, err := time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("--since: %w", err)
				}
				f.Since = time.Now().Add(-d)
			}
			recs, err := audit.Query(f)
			if err != nil {
				return err
			}
			s := audit.Summarize(recs)
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "total %d\n", s.Total)
			fmt.Fprintf(out, "by risk: %v\n", s.ByRisk)
			fmt.Fprintf(out, "by tool: %v\n", s.ByTool)
			fmt.Fprintf(out, "by seat: %v\n", s.BySeat)
			return nil
		},
	}
	c.Flags().StringVar(&project, "project", "", "filter by project")
	c.Flags().StringVar(&since, "since", "", "window (e.g. 24h)")
	return c
}
```

Add `audit.Prune(olderThan time.Duration) (int, error)` to `internal/audit/audit.go` (deletes whole day-files older than the cutoff across all projects) and a `newAuditPruneCmd()` calling it. Register all three in `newAuditCmd`:

```go
cmd.AddCommand(newAuditHookCmd(), newAuditLogCmd(), newAuditSummaryCmd(), newAuditPruneCmd())
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/pactify/ -run Audit`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/pactify/cmd_audit.go cmd/pactify/cmd_audit_test.go internal/audit/audit.go internal/audit/audit_test.go
git commit -m "feat(audit): audit log/summary/prune CLI"
```

---

## Task 7: `audit install --claude-code` (project-scoped settings upsert) — owner claude (deps: T4)

**Files:**
- Create: `internal/audit/install.go`
- Test: `internal/audit/install_test.go`
- Modify: `cmd/pactify/cmd_audit.go` (add `install`/`uninstall`/`detect` subcommands)

- [ ] **Step 1: Write the failing test**

```go
package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallClaudeCodeIdempotent(t *testing.T) {
	repo := t.TempDir()
	// Install twice → exactly one pact audit PreToolUse entry.
	if err := Install("claude-code", repo); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := Install("claude-code", repo); err != nil {
		t.Fatalf("install (2nd): %v", err)
	}
	b, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("settings not valid JSON: %v", err)
	}
	hooks := s["hooks"].(map[string]any)["PreToolUse"].([]any)
	n := 0
	for _, h := range hooks {
		entry := h.(map[string]any)
		for _, hh := range entry["hooks"].([]any) {
			if cmd, _ := hh.(map[string]any)["command"].(string); containsStr(cmd, "audit hook") {
				n++
			}
		}
	}
	if n != 1 {
		t.Fatalf("want exactly 1 audit hook entry after 2 installs, got %d", n)
	}
}

func TestUninstallRemovesEntry(t *testing.T) {
	repo := t.TempDir()
	_ = Install("claude-code", repo)
	if err := Uninstall("claude-code", repo); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(repo, ".claude", "settings.json"))
	if containsStr(string(b), "audit hook") {
		t.Fatalf("audit hook still present after uninstall: %s", b)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/audit/ -run Install`
Expected: FAIL — `undefined: Install` / `Uninstall`.

- [ ] **Step 3: Write minimal implementation**

```go
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// hookCommand is the command a client invokes per tool call. Stable across
// rebuilds: it calls the `pactify` on PATH (install assumes pactify is installed).
func hookCommand(kind string) string { return "pactify audit hook --kind " + kind }

// Install registers the project-scoped PreToolUse audit hook for kind at
// repoDir/.claude/settings.json (claude-code). Idempotent: a prior pact audit
// entry is removed before inserting. Other kinds: see Task 8.
func Install(kind, repoDir string) error {
	switch kind {
	case "claude-code", "opencode":
		return installClaudeStyle(kind, filepath.Join(repoDir, ".claude", "settings.json"))
	default:
		return fmt.Errorf("audit install: unsupported kind %q", kind)
	}
}

// Uninstall removes the audit hook entry for kind, leaving other hooks intact.
func Uninstall(kind, repoDir string) error {
	return uninstallClaudeStyle(filepath.Join(repoDir, ".claude", "settings.json"))
}

func installClaudeStyle(kind, settingsPath string) error {
	s := readSettings(settingsPath)
	hooks := mapOf(s, "hooks")
	pre := sliceOf(hooks, "PreToolUse")
	pre = dropAuditEntries(pre)
	pre = append(pre, map[string]any{
		"matcher": "*",
		"hooks":   []any{map[string]any{"type": "command", "command": hookCommand(kind)}},
	})
	hooks["PreToolUse"] = pre
	s["hooks"] = hooks
	return writeSettings(settingsPath, s)
}

func uninstallClaudeStyle(settingsPath string) error {
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		return nil
	}
	s := readSettings(settingsPath)
	hooks := mapOf(s, "hooks")
	hooks["PreToolUse"] = dropAuditEntries(sliceOf(hooks, "PreToolUse"))
	s["hooks"] = hooks
	return writeSettings(settingsPath, s)
}

// dropAuditEntries removes any PreToolUse entry whose command contains "audit hook".
func dropAuditEntries(entries []any) []any {
	out := entries[:0:0]
	for _, e := range entries {
		if isAuditEntry(e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func isAuditEntry(e any) bool {
	m, ok := e.(map[string]any)
	if !ok {
		return false
	}
	hh, ok := m["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range hh {
		if cmd, _ := mapAny(h)["command"].(string); containsStr(cmd, "audit hook") {
			return true
		}
	}
	return false
}

// --- tiny JSON-as-map helpers (settings.json is user-owned; preserve unknown keys) ---

func readSettings(path string) map[string]any {
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var s map[string]any
	if json.Unmarshal(b, &s) != nil || s == nil {
		return map[string]any{}
	}
	return s
}

func writeSettings(path string, s map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func mapOf(s map[string]any, k string) map[string]any {
	if v, ok := s[k].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}
func sliceOf(s map[string]any, k string) []any {
	if v, ok := s[k].([]any); ok {
		return v
	}
	return []any{}
}
func mapAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
```

Add `Detect(repoDir string) []Status` returning per-kind install state (kind + installed bool), and CLI `newAuditInstallCmd`/`newAuditUninstallCmd`/`newAuditDetectCmd` (flags `--claude-code`/`--opencode`, default repoDir = cwd). Register in `newAuditCmd`. `install --detect` warns if a non-pact PreToolUse hook (e.g. AgentPact) is already present.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/audit/ -run Install`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/audit/install.go internal/audit/install_test.go cmd/pactify/cmd_audit.go
git commit -m "feat(audit): audit install/uninstall/detect (claude-code, idempotent, project-scoped)"
```

---

## Task 8: opencode install branch (实测 first) — owner claude (deps: T7)

**Files:**
- Modify: `internal/audit/install.go`, `internal/audit/hook.go` (mapTool opencode branch if fields differ)
- Test: `internal/audit/install_test.go`

- [ ] **Step 1: 实测 opencode's PreToolUse hook** (gate per CLAUDE.md — no asserting from docs)

Run: `opencode --help` and inspect opencode's settings/hook docs. Determine:
- Does opencode emit PreToolUse hooks? Config file location + JSON shape.
- The stdin field names for its tool calls (command / file path).

Record findings in the task spec's handoff log. **If opencode's hook config differs
from claude-code's `.claude/settings.json` shape, the `installClaudeStyle` reuse in
Task 7 is wrong for opencode** — split into an `installOpencode` writing opencode's
real config path/shape, and add an opencode branch to `mapTool` for its field names.

- [ ] **Step 2: Write the failing test** (opencode install writes opencode's real config)

```go
func TestInstallOpencode(t *testing.T) {
	repo := t.TempDir()
	if err := Install("opencode", repo); err != nil {
		t.Fatalf("install opencode: %v", err)
	}
	// Assert the file + shape that 实测 confirmed (path/keys filled in after Step 1).
	// e.g. opencode config at <path>, containing the "audit hook --kind opencode" command.
}
```

- [ ] **Step 3: Implement the verified opencode branch** (path + shape from Step 1).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/audit/ -run Install`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/audit/install.go internal/audit/hook.go internal/audit/install_test.go
git commit -m "feat(audit): opencode install branch (verified hook shape)"
```

---

## Task 9: serve `GET /api/projects/{id}/audit` — owner claude (deps: T2)

**Files:**
- Create: `internal/serve/audit.go`
- Test: `internal/serve/audit_test.go`
- Modify: serve route registration (where `registerSessionRoutes` is wired)

- [ ] **Step 1: Write the failing test**

```go
package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentjoey/pactify/internal/audit"
)

func TestHandleAuditList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	_ = audit.Append(audit.Record{Project: "demo", TS: "2026-06-16T01:00:00Z", Seat: "dev", Task: "t1", Tool: "bash", Risk: "exec"})

	s := &Server{}
	r := httptest.NewRequest("GET", "/api/projects/demo/audit?seat=dev", nil)
	r.SetPathValue("id", "demo")
	w := httptest.NewRecorder()
	s.handleAudit(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var recs []audit.Record
	json.NewDecoder(w.Body).Decode(&recs)
	if len(recs) != 1 || recs[0].Tool != "bash" {
		t.Fatalf("resp = %+v", recs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/serve/ -run Audit`
Expected: FAIL — `undefined: handleAudit`.

- [ ] **Step 3: Write minimal implementation**

```go
package serve

import (
	"net/http"
	"time"

	"github.com/agentjoey/pactify/internal/audit"
)

func (s *Server) registerAuditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/audit", s.handleAudit)
}

// handleAudit returns audit records for a project, filtered by query params
// (seat, task, session, risk, since=<dur>). Read-only.
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := audit.Filter{
		Project: r.PathValue("id"),
		Seat:    q.Get("seat"),
		Task:    q.Get("task"),
		Session: q.Get("session"),
		Risk:    q.Get("risk"),
	}
	if since := q.Get("since"); since != "" {
		if d, err := time.ParseDuration(since); err == nil {
			f.Since = time.Now().Add(-d)
		}
	}
	recs, err := audit.Query(f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if recs == nil {
		recs = []audit.Record{}
	}
	writeJSON(w, http.StatusOK, recs)
}
```

Wire `s.registerAuditRoutes(mux)` alongside the other `register*Routes` calls.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/serve/ -run Audit`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/serve/audit.go internal/serve/audit_test.go internal/serve/*.go
git commit -m "feat(serve): GET /api/projects/{id}/audit (filtered, read-only)"
```

---

## Task 10: Dashboard Audit lens — owner claude (deps: T9)

**Files:**
- Create: `web/src/components/AuditLens.tsx`, `web/src/components/AuditLens.test.tsx`
- Modify: `web/src/lib/api.ts` (`getAudit`), and the host that renders lenses (RightRail or OfficeView, beside the Cost lens)

- [ ] **Step 1: Write the failing test**

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

const getAudit = vi.fn();
vi.mock("../lib/api", () => ({ getAudit: (...a: unknown[]) => getAudit(...a) }));

import { AuditLens } from "./AuditLens";

describe("AuditLens", () => {
  it("renders audit records grouped with counts", async () => {
    getAudit.mockResolvedValue([
      { ts: "2026-06-16T02:00:00Z", seat: "dev", task: "t1", tool: "bash", risk: "exec", summary: "go build", decision: "allow" },
      { ts: "2026-06-16T01:00:00Z", seat: "dev", task: "t1", tool: "fs.read", risk: "read", summary: "/x.go", decision: "allow" },
    ]);
    render(<AuditLens project="demo" task="t1" />);
    await waitFor(() => expect(screen.getByText("go build")).toBeTruthy());
    expect(screen.getByText(/bash/)).toBeTruthy();
    expect(screen.getByTestId("audit-count")).toHaveTextContent("2");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npm test -- AuditLens`
Expected: FAIL — cannot find `./AuditLens` / `getAudit`.

- [ ] **Step 3: Write minimal implementation**

Add to `web/src/lib/api.ts`:

```ts
export interface AuditRecord {
  ts: string; project?: string; repo?: string; seat: string; task: string;
  kind?: string; session?: string; tool: string; summary: string; risk: string; decision: string;
}
export const getAudit = (project: string, params: Record<string, string> = {}) => {
  const qs = new URLSearchParams(params).toString();
  return getJSON<AuditRecord[]>(`/api/projects/${project}/audit${qs ? `?${qs}` : ""}`);
};
```

Create `web/src/components/AuditLens.tsx`: fetch `getAudit(project, {task, seat})`, render a count badge (`data-testid="audit-count"`) + a risk-colored row per record (ts · seat · tool · summary), styled with the existing tokens to sit beside the Cost lens. Read-only.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npm test -- AuditLens`
Expected: PASS. Then full gate: `cd web && npm test` (vitest) + `npm run e2e` (Playwright) double-green.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/AuditLens.tsx web/src/components/AuditLens.test.tsx web/src/lib/api.ts web/src/components/*.tsx
git commit -m "feat(web): dashboard Audit lens (per-task tool-call timeline + counts)"
```

---

## Execution (orchestrate, not subagent-driven)

This plan is driven by **`pactify orchestrate`** with two seats — claude
(orchestrator/reviewer + complex owner) and an opencode worker seat — per the task
table's owner/reviewer/deps. Run on a feature branch off `feat-light-theme`:

1. Set up the pact tasks (`pactify assign` each Task with owner/reviewer/deps and a
   `verify:` line = the table's command) — or hand-drive with claude as orchestrator.
2. `pactify orchestrate --idle-timeout 5 --run-timeout 30` (session cleanup ON by
   default — that's the side-observation target).
3. After each opencode-owned task (T1/T2/T6) is accepted, run `opencode session list`
   and confirm no `pact:<opencode-seat>` session remains (cleanup verification).
4. Gate per phase: `go test ./...` (+ `cd web && npm test && npm run e2e` for T10).

---

## Self-Review

**Spec coverage:** §4 interception → T3/T4; §5 record → T1; §6 store → T1/T2 (+prune
T6); §7 correlation → T5; §8.1 audit pkg → T1/T2/T3; §8.2 redaction → T3; §8.3 CLI →
T4/T6/T7; §8.4 serve+lens → T9/T10; §9 install → T7/T8; §11 coverage (claude+opencode)
→ T7/T8; §12 testing → every task is TDD; §13 verify items → T8 step 1 (opencode hook
实测) + execution step 3. **Gap check:** `audit.Prune` is introduced in T6 (used by the
prune subcommand) — covered. `Detect` introduced in T7 — covered. No spec section
without a task.

**Placeholder scan:** No TBD/TODO. T8 is intentionally a 实测-gated task (its exact
opencode path/shape is unknown until probed) — this is a verification step, not a
placeholder; its test/impl are written after the probe, per the 禁止凭文档断言 rule.

**Type consistency:** `Record`/`Filter`/`Summary` fields identical across T1/T2/T6/T9/T10
(JSON tags match the lens's `AuditRecord`). `LaunchContext{Seat,Kind,Task,Project,Briefing,RepoDir}`
defined T5, consumed by runner; `Env{Seat,Task,Project}` + `FromHook(kind,stdin,env,now)`
consistent T3↔T4. `hookCommand(kind)` → `"pactify audit hook --kind <kind>"` matches the
install-entry detection (`contains "audit hook"`) and the cmd flag in T4.
