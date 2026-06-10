// Package doctor runs adoption health checks reused by `pactify doctor` and `pactify setup`.
package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
)

type Check struct {
	Name   string
	OK     bool
	Detail string // remediation hint when !OK, or info when OK
}

// checkPath reports whether the directory of exePath is on pathEnv.
func checkPath(exePath, pathEnv string) Check {
	dir := filepath.Dir(exePath)
	for _, p := range strings.Split(pathEnv, string(os.PathListSeparator)) {
		if p == dir {
			return Check{"pactify on PATH", true, dir}
		}
	}
	return Check{"pactify on PATH", false, fmt.Sprintf("%s is not on PATH; add it or re-run install.sh", dir)}
}

func checkSeat(agentID string) Check {
	if agentID == "" {
		return Check{"PACT_AGENT_ID set", false, "export PACT_AGENT_ID=<seat> (needed for shell verbs)"}
	}
	return Check{"PACT_AGENT_ID set", true, agentID}
}

// checkRepo requires cwd to be the process working directory for the validate
// call (pact.Validate reads via paths.Dir(), which is cwd-relative). A caller
// passing any other dir would silently validate the wrong repo, so that is a
// loud failure instead.
func checkRepo(cwd string) Check {
	if wd, err := os.Getwd(); err != nil || wd != cwd {
		return Check{".pact/ valid", false, "internal: doctor must run with cwd = process working directory"}
	}
	if _, err := os.Stat(filepath.Join(cwd, paths.Dir())); err != nil {
		return Check{".pact/ present", false, "no .pact/ here — run `pactify setup` or `pactify init`"}
	}
	if err := pact.Validate(); err != nil {
		return Check{".pact/ valid", false, "validate failed: " + err.Error()}
	}
	return Check{".pact/ valid", true, "protocol v1 conformant"}
}

func checkAgentWiring(cwd string) Check {
	// JSON configs must contain a "pact" server key; markdown entries must
	// contain the pact managed-block marker.
	type probe struct{ file, marker string }
	probes := []probe{
		{"opencode.json", `"pact"`},
		{".mcp.json", `"pact"`},
		{".gemini/settings.json", `"pact"`},
		{"AGENTS.md", "pact:begin"},
		{"CLAUDE.md", "pact:begin"},
		{"GEMINI.md", "pact:begin"},
	}
	var found []string
	for _, p := range probes {
		b, err := os.ReadFile(filepath.Join(cwd, p.file))
		if err == nil && strings.Contains(string(b), p.marker) {
			found = append(found, p.file)
		}
	}
	if len(found) == 0 {
		return Check{"agent wiring", false, "no pact wiring here — `pactify agent add <kind>`"}
	}
	return Check{"agent wiring", true, "found: " + strings.Join(found, ", ")}
}

// Run executes the non-MCP checks. The MCP-launch check (spec B1 #5) is run by the
// command layer's checkMCP, which spawns the PATH-resolved binary and completes an
// initialize handshake; keeping it there leaves this package pure and unit-testable.
func Run(cwd, agentID, exePath, pathEnv string) []Check {
	return []Check{
		checkPath(exePath, pathEnv),
		checkRepo(cwd),
		checkSeat(agentID),
		checkAgentWiring(cwd),
	}
}
