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
// call (pact.Validate reads via paths.Dir(), which is cwd-relative).
func checkRepo(cwd string) Check {
	if _, err := os.Stat(filepath.Join(cwd, paths.Dir())); err != nil {
		return Check{".pact/ present", false, "no .pact/ here — run `pactify setup` or `pactify init`"}
	}
	if err := pact.Validate(); err != nil {
		return Check{".pact/ valid", false, "validate failed: " + err.Error()}
	}
	return Check{".pact/ valid", true, "protocol v1 conformant"}
}

func checkAgentWiring(cwd string) Check {
	candidates := []string{"opencode.json", ".mcp.json", ".gemini/settings.json", "AGENTS.md", "CLAUDE.md", "GEMINI.md"}
	var found []string
	for _, f := range candidates {
		if _, err := os.Stat(filepath.Join(cwd, f)); err == nil {
			found = append(found, f)
		}
	}
	if len(found) == 0 {
		return Check{"agent wiring", false, "no agent config here — `pactify agent add <kind>`"}
	}
	return Check{"agent wiring", true, "found: " + strings.Join(found, ", ")}
}

// Run executes the non-MCP checks. The MCP-launch check is run by the command layer
// (it spawns the binary) so this stays pure and unit-testable.
func Run(cwd, agentID, exePath, pathEnv string) []Check {
	return []Check{
		checkPath(exePath, pathEnv),
		checkRepo(cwd),
		checkSeat(agentID),
		checkAgentWiring(cwd),
	}
}
