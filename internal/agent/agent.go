// Package agent wires the pact MCP server into each supported agent (CLI and
// desktop-app surfaces) and renders their onboarding blocks.
package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Format int

const (
	JSONMcpServers Format = iota // {"mcpServers":{...}} command/args/env
	JSONOpencode                 // {"mcp":{...}} type/command[]/environment
	TOML                         // [mcp_servers.x] — doc-only this milestone
)

type Scope int

const (
	Project Scope = iota // a file committed in the repo
	Global               // a machine-level file outside the repo
)

type ConfigTarget struct {
	Path   string // repo-relative (Project) or ~/absolute template (Global)
	Scope  Scope
	Format Format
}

type Invoke struct {
	Command string
	Args    []string
	Env     map[string]string
}

// RunnerSpec describes how to launch this kind's agent non-interactively
// (headless) with a briefing prompt. This is distinct from Invocation(), which
// is how the agent's config invokes the pact MCP server. Args carries a
// "{briefing}" placeholder that the caller replaces with the real prompt text.
type RunnerSpec struct {
	Command string   // "opencode" / "claude" / "gemini"
	Args    []string // e.g. ["run","{briefing}"] / ["-p","{briefing}"]
}

type Adapter interface {
	Kind() string
	DefaultEntry() string // "" = no entry file (pure desktop app)
	Config() ConfigTarget
	Invocation(seatID, repoAbs string) Invoke
	// Runner returns this kind's headless runner spec; ok=false means the kind
	// has no headless runner (GUI/desktop, or unverified CLI).
	Runner() (RunnerSpec, bool)
}

type spec struct {
	kind       string
	entry      string
	cfgPath    string
	scope      Scope
	format     Format
	desktop    bool
	runnerCmd  string   // "" = no headless runner
	runnerArgs []string // includes "{briefing}" placeholder
}

func (s spec) Kind() string         { return s.kind }
func (s spec) DefaultEntry() string { return s.entry }
func (s spec) Config() ConfigTarget {
	return ConfigTarget{Path: s.cfgPath, Scope: s.scope, Format: s.format}
}

func (s spec) Invocation(seatID, repoAbs string) Invoke {
	args := []string{"mcp"}
	if s.desktop {
		args = append(args, "--project", repoAbs)
	}
	return Invoke{Command: "pactify", Args: args, Env: map[string]string{"PACT_AGENT_ID": seatID}}
}

// Runner returns the headless runner spec for this kind; ok=false when runnerCmd
// is empty (GUI/desktop kinds, or CLIs whose headless mode is unverified).
func (s spec) Runner() (RunnerSpec, bool) {
	if s.runnerCmd == "" {
		return RunnerSpec{}, false
	}
	return RunnerSpec{Command: s.runnerCmd, Args: s.runnerArgs}, true
}

var registry = map[string]spec{
	"opencode":       {"opencode", "AGENTS.md", "opencode.json", Project, JSONOpencode, false, "opencode", []string{"run", "{briefing}"}},
	// claude-code: -p is non-interactive (headless); --dangerously-skip-permissions
	// is REQUIRED for autonomous tool use (no human to approve Edit/Bash) — without
	// it `claude -p` stalls on permission prompts and cannot develop/review.
	"claude-code":    {"claude-code", "CLAUDE.md", ".mcp.json", Project, JSONMcpServers, false, "claude", []string{"-p", "--dangerously-skip-permissions", "{briefing}"}},
	"gemini-cli":     {"gemini-cli", "GEMINI.md", ".gemini/settings.json", Project, JSONMcpServers, false, "gemini", []string{"-p", "{briefing}"}},
	// codex-cli: codex headless not yet verified; keep no runner conservatively
	// (set runnerCmd once `codex exec`-style headless mode is confirmed).
	"codex-cli":      {"codex-cli", "AGENTS.md", ".codex/config.toml", Project, TOML, false, "", nil},
	"claude-desktop": {"claude-desktop", "", "~/Library/Application Support/Claude/claude_desktop_config.json", Global, JSONMcpServers, true, "", nil},
	"antigravity":    {"antigravity", "", "~/.gemini/config/mcp_config.json", Global, JSONMcpServers, true, "", nil},
	"codex-app":      {"codex-app", "AGENTS.md", "~/.codex/config.toml", Global, TOML, true, "", nil},
}

// Get returns the adapter for kind.
func Get(kind string) (Adapter, bool) {
	s, ok := registry[kind]
	return s, ok
}

// Kinds returns the supported kinds, sorted.
func Kinds() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ExpandPath expands a leading ~ to the user's home dir; other paths pass through.
func ExpandPath(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}
