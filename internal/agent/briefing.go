package agent

import (
	"fmt"

	"github.com/agentjoey/pactify/internal/pact"
)

// briefing renders the agent-agnostic onboarding body (MCP-first, shell fallback).
func briefing(seatID, roles string) string {
	return fmt.Sprintf(`# pact protocol — seat `+"`%s`"+`

This repo uses the **pact protocol** (v1). You are seat `+"`%s`"+`, roles: %s.

**Primary — MCP:** the `+"`pact`"+` MCP server is wired into your config. Use its tools
(status / join / assign / checkpoint / accept / changes / merge / list) and resources
(`+"`pact://state`"+`, `+"`pact://log`"+`). Cold start: call `+"`status`"+`, then `+"`join`"+`
(registers your seat and checks out your feature branch).

**Fallback — shell** (if MCP is unavailable):
`+"```bash"+`
export PACT_AGENT_ID=%s
pactify join %s --roles %s
`+"```"+`
then `+"`pactify help`"+` for the verbs.

**The two rules:** a worker cannot self-accept (only the task's reviewer accepts); a
feature cannot merge until all its tasks are accepted.`, seatID, seatID, roles, seatID, seatID, roles)
}

// Render returns the entry-file block (empty if the kind has no entry file) and
// the config snippet for a kind. Pure: no IO.
func Render(kind, seatID, roles, repoAbs string) (entry, config string, err error) {
	a, ok := Get(kind)
	if !ok {
		return "", "", fmt.Errorf("unknown agent kind %q (supported: %v)", kind, Kinds())
	}
	if a.DefaultEntry() != "" {
		entry = briefing(seatID, roles)
	}
	config = snippet(a.Config().Format, a.Invocation(seatID, repoAbs))
	return entry, config, nil
}

// BakeEntry delegates to pact.BakeManagedBlock so callers can write the managed
// block into a seat's entry file without importing pact directly.
func BakeEntry(path, body string) error {
	return pact.BakeManagedBlock(path, body)
}
