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

// antigravityInput is antigravity's (agy) PreToolUse payload. agy is
// gemini-lineage but does NOT share gemini-cli's hook shape: the payload is
// protojson camelCase with the call nested under `toolCall`, the session is
// `conversationId`, and there is no `cwd` — the workspace arrives as
// `workspacePaths`. Verified against a real agy 1.1.19 run (docs/backlog.md
// [AUDIT]).
type antigravityInput struct {
	ToolCall struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"toolCall"`
	ConversationID string   `json:"conversationId"`
	WorkspacePaths []string `json:"workspacePaths"`
}

// normalizeAntigravity flattens agy's payload into the claude-shaped hookInput
// the rest of the pipeline speaks. ok=false on malformed input or a payload with
// no tool name — including a claude-shaped payload, which must not silently
// half-parse into an attribution-less record.
func normalizeAntigravity(stdin []byte) (hookInput, bool) {
	var in antigravityInput
	if json.Unmarshal(stdin, &in) != nil || in.ToolCall.Name == "" {
		return hookInput{}, false
	}
	cwd := ""
	if len(in.WorkspacePaths) > 0 {
		cwd = in.WorkspacePaths[0]
	}
	return hookInput{
		ToolName:  in.ToolCall.Name,
		ToolInput: in.ToolCall.Args,
		SessionID: in.ConversationID,
		Cwd:       cwd,
	}, true
}

// FromHook parses a client's PreToolUse stdin into a Record, stamping env +
// session/cwd. ok=false when the tool is unmapped or the input is malformed (the
// caller then no-ops, exit 0). Pure: (kind, bytes, env, now) → Record.
func FromHook(kind string, stdin []byte, env Env, now time.Time) (Record, bool) {
	var in hookInput
	if kind == "antigravity" {
		var ok bool
		if in, ok = normalizeAntigravity(stdin); !ok {
			return Record{}, false
		}
	} else if json.Unmarshal(stdin, &in) != nil || in.ToolName == "" {
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
		URL      string `json:"url"`
		Query    string `json:"query"`
		// antigravity (agy) uses PascalCase arg names and a different word for
		// each tool's subject. Captured from real agy 1.1.19 tool calls:
		// run_command{CommandLine,Cwd} · view_file{AbsolutePath} ·
		// write_to_file{TargetFile,CodeContent} ·
		// replace_file_content{TargetFile,ReplacementContent} ·
		// list_dir{DirectoryPath} · grep_search{Query,SearchPath} ·
		// find_by_name{Pattern,SearchDirectory}.
		CommandLine   string `json:"CommandLine"`
		TargetFile    string `json:"TargetFile"`
		AbsolutePath  string `json:"AbsolutePath"`
		DirectoryPath string `json:"DirectoryPath"`
		Pattern       string `json:"Pattern"`
	}
	_ = json.Unmarshal(rawInput, &fields)
	command := fields.Command
	if command == "" {
		command = fields.CommandLine
	}
	path := firstNonEmpty(fields.FilePath, fields.TargetFile, fields.AbsolutePath,
		fields.DirectoryPath, fields.Path, fields.URL, fields.Pattern, fields.Query)
	switch name {
	case "Bash", "run_shell_command", "run_command":
		return "bash", command, "exec", true
	case "Write", "Edit", "MultiEdit", "NotebookEdit", "write_file", "replace",
		// agy write-side tools. delete_directory is destructive, so it is
		// recorded at write risk rather than dropped.
		"write_to_file", "replace_file_content", "edit_notebook", "delete_directory":
		return "fs.write", path, "write", true
	case "Read", "read_file", "read_many_files", "google_web_search", "web_fetch",
		// agy read-side tools.
		"view_file", "view_file_outline", "view_code_item", "view_content_chunk",
		"list_dir", "list_directory", "grep_search", "find_by_name", "codebase_search",
		"read_notebook", "read_url_content", "search_web":
		return "fs.read", path, "read", true
	default:
		if strings.HasPrefix(name, "mcp__") {
			return name, name, "mcp", true
		}
		return "", "", "", false
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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
	// Slack tokens: keep the xox<prefix>- introducer and mask the rest.
	{regexp.MustCompile(`\b(xox[baprs]-)[A-Za-z0-9-]{10,}\b`), `$1***`},
	// Google API keys: AIza followed by exactly 35 alphanumeric/url-safe chars.
	{regexp.MustCompile(`\b(AIza)[0-9A-Za-z_\-]{35}\b`), `$1***`},
	// Stripe live/test keys: keep sk/rk/pk_live/test_ prefix and mask the secret.
	{regexp.MustCompile(`\b((?:sk|rk|pk)_(?:live|test)_)[A-Za-z0-9]{10,}\b`), `$1***`},
	// GCP OAuth access tokens: ya29.<base64url>.
	{regexp.MustCompile(`\b(ya29\.)([A-Za-z0-9_\-]+)`), `$1***`},
	// JWT: three base64url segments.
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\b`), `***`},
	// PEM private-key header: keep a generic BEGIN header and mask the rest.
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`), `-----BEGIN PRIVATE KEY----- ***`},
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
