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
// names are a verification item — adjust this branch after probing (see Task 8).
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

// secretRes are applied in order; each keeps the introducer and replaces the
// secret run with "***". Summaries land in plaintext under ~/.pactify/audit,
// so the net must catch env-var/flag assignments and well-known token shapes,
// while KEY=VALUE stays anchored to a token start (start / whitespace / quote)
// so ordinary words like "feature/keyboard" pass through untouched.
var secretRes = []struct {
	re   *regexp.Regexp
	repl string
}{
	// scheme://user:pass@host — mask the basic-auth credentials part.
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.\-]*://)[^\s/@:]+:[^\s/@]+@`), `$1***@`},
	// KEY=VALUE where KEY smells secret-ish (AWS_SECRET_ACCESS_KEY=, --api-key=,
	// --password=, PGPASSWORD=, PWD-suffixed vars, …).
	{regexp.MustCompile(`(?i)(^|[\s"'])([A-Za-z0-9_\-]*(?:key|token|secret|password|passwd|pwd)[A-Za-z0-9_\-]*=)[^\s"']+`), `$1$2***`},
	// bare well-known token shapes: GitHub tokens / PATs, AWS access key ids.
	{regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16})\b`), `***`},
	{regexp.MustCompile(`(?i)(bearer\s+|token[=:]\s*|secret[=:]\s*|sk-)[A-Za-z0-9._\-]+`), `$1***`},
}

// redact masks secret-ish runs and truncates to summaryCap chars.
func redact(s string) string {
	for _, r := range secretRes {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	if len(s) > summaryCap {
		s = s[:summaryCap] + "…"
	}
	return s
}
