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
	kind      string
	entry     string
	cfgPath   string
	scope     Scope
	format    Format
	desktop   bool
	detectBin string // CLI binary to LookPath for install detection; "" = desktop kind (detect via Config().Path)
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

// Runner returns the headless runner spec for this kind; ok=false for kinds with
// no verified headless runner. It delegates to the kind's RunnerProfile (single
// source of truth for launch argv) rendered with the default model and the
// default blanket permission posture, preserving the historical Args contract.
// agentcfg.Resolve is the parametric path used by orchestrate (model/posture
// overrides); Runner() is the zero-config default kept for back-compat callers.
func (s spec) Runner() (RunnerSpec, bool) {
	p, ok := RunnerProfileFor(s.kind)
	if !ok {
		return RunnerSpec{}, false
	}
	return RunnerSpec{Command: p.Command, Args: p.BuildArgs(p.DefaultModel, PermPosture{}, briefingToken)}, true
}

// briefingToken is the placeholder Runner() emits in its default Args (callers
// substitute the real prompt). agentcfg/orchestrate render the briefing directly.
const briefingToken = "{briefing}"

var registry = map[string]spec{
	"opencode":       {"opencode", "AGENTS.md", "opencode.json", Project, JSONOpencode, false, "opencode"},
	"claude-code":    {"claude-code", "CLAUDE.md", ".mcp.json", Project, JSONMcpServers, false, "claude"},
	"gemini-cli":     {"gemini-cli", "GEMINI.md", ".gemini/settings.json", Project, JSONMcpServers, false, "gemini"},
	"codex-cli":      {"codex-cli", "AGENTS.md", ".codex/config.toml", Project, TOML, false, "codex"},
	"kimi-cli":       {"kimi-cli", "AGENTS.md", "~/.kimi/mcp.json", Global, JSONMcpServers, false, "kimi"},
	"cursor-cli":     {"cursor-cli", "AGENTS.md", ".cursor/mcp.json", Project, JSONMcpServers, false, "cursor-agent"},
	"claude-desktop": {"claude-desktop", "", "~/Library/Application Support/Claude/claude_desktop_config.json", Global, JSONMcpServers, true, ""},
	"antigravity":    {"antigravity", "", "~/.gemini/config/mcp_config.json", Global, JSONMcpServers, true, ""},
	"codex-app":      {"codex-app", "AGENTS.md", "~/.codex/config.toml", Global, TOML, true, ""},
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
