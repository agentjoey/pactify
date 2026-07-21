// Package doctor runs adoption health checks reused by `pactify doctor` and `pactify setup`.
package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
)

type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"` // remediation hint when !OK, or info when OK
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

// checkSeat reports the resolved acting identity. agentID is the resolved seat
// (env or .pact/seat file — see paths.AgentIDIn); empty means unresolved.
func checkSeat(agentID string) Check {
	if agentID == "" {
		return Check{"seat identity", false, "unresolved — `pactify seat use <id>` (binds this working copy) or export PACT_AGENT_ID=<seat>"}
	}
	return Check{"seat identity", true, agentID}
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

// checkAgentWiring folds the shared content-aware probe (agent.ProbeWiring) into
// the doctor Check. A kind is wired when its config carries the pact server key
// OR its entry file carries the managed-block marker; the Detail surfaces which
// file proved each wired kind ("config opencode.json" / "entry CLAUDE.md").
// Global (machine-level) kinds are skipped: doctor reports per-repo wiring, and
// a machine-global config would otherwise mark every repo "wired".
func checkAgentWiring(cwd string) Check {
	var found []string
	for _, ws := range agent.ProbeWiring(cwd) {
		if ws.Wired && !ws.Global {
			found = append(found, ws.Detail)
		}
	}
	if len(found) == 0 {
		return Check{"agent wiring", false, "no pact wiring here — `pactify agent add <kind>`"}
	}
	return Check{"agent wiring", true, "found: " + strings.Join(found, ", ")}
}

// checkPinnedIdentity warns when a project config still hard-codes a seat id in
// the pact server's env (legacy pre-seat-identity wiring). A pinned identity
// blocks a second same-kind seat (last-writer-wins collision) and overrides an
// orchestrate-injected identity; re-wiring drops it (spec seat-identity §3.4).
func checkPinnedIdentity(cwd string) Check {
	pinned := agent.PinnedIdentity(cwd)
	if len(pinned) == 0 {
		return Check{"seat identity wiring", true, "no config pins a seat id"}
	}
	var parts []string
	for _, p := range pinned {
		parts = append(parts, fmt.Sprintf("%s pins %q in %s", p.Kind, p.Seat, p.Path))
	}
	return Check{"seat identity wiring", false,
		strings.Join(parts, "; ") + " — re-wire to drop the pinned identity: `pactify agent add <kind>` (identity now resolves from PACT_AGENT_ID or `pactify seat use`)"}
}

// Run executes the non-MCP checks. The MCP-launch check (spec B1 #5) is run by the
// command layer's checkMCP, which spawns the PATH-resolved binary and completes an
// initialize handshake; keeping it there leaves this package pure and unit-testable.
//
// home is the user's home directory (injected, not read from os.Getenv here) so
// the per-vendor auth checks stay hermetically testable.
func Run(cwd, agentID, exePath, pathEnv, home string) []Check {
	checks := []Check{
		checkPath(exePath, pathEnv),
		checkRepo(cwd),
		checkSeat(agentID),
		checkAgentWiring(cwd),
		checkPinnedIdentity(cwd),
	}
	return append(checks, VendorChecks(home, pathEnv)...)
}
