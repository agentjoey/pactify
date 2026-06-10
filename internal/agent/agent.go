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

type Adapter interface {
	Kind() string
	DefaultEntry() string // "" = no entry file (pure desktop app)
	Config() ConfigTarget
	Invocation(seatID, repoAbs string) Invoke
}

type spec struct {
	kind    string
	entry   string
	cfgPath string
	scope   Scope
	format  Format
	desktop bool
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

var registry = map[string]spec{
	"opencode":       {"opencode", "AGENTS.md", "opencode.json", Project, JSONOpencode, false},
	"claude-code":    {"claude-code", "CLAUDE.md", ".mcp.json", Project, JSONMcpServers, false},
	"gemini-cli":     {"gemini-cli", "GEMINI.md", ".gemini/settings.json", Project, JSONMcpServers, false},
	"codex-cli":      {"codex-cli", "AGENTS.md", ".codex/config.toml", Project, TOML, false},
	"claude-desktop": {"claude-desktop", "", "~/Library/Application Support/Claude/claude_desktop_config.json", Global, JSONMcpServers, true},
	"antigravity":    {"antigravity", "", "~/.gemini/config/mcp_config.json", Global, JSONMcpServers, true},
	"codex-app":      {"codex-app", "AGENTS.md", "~/.codex/config.toml", Global, TOML, true},
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
