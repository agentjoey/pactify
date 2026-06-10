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

// Wire performs the onboarding side effects for kind: bake the entry block (if
// the kind has an entry file) and, for JSON kinds, merge the pact server into the
// agent's config. TOML kinds are doc-only — their config is not written (the
// caller prints the snippet). Project-scoped paths are relative to cwd.
func Wire(kind, seatID, roles, repoAbs string) error {
	a, ok := Get(kind)
	if !ok {
		return fmt.Errorf("unknown agent kind %q (supported: %v)", kind, Kinds())
	}
	if a.DefaultEntry() != "" {
		if err := pact.BakeManagedBlock(a.DefaultEntry(), briefing(seatID, roles)); err != nil {
			return err
		}
	}
	c := a.Config()
	if c.Format == TOML {
		return nil // doc-only
	}
	return mergeServer(ExpandPath(c.Path), c.Format, a.Invocation(seatID, repoAbs))
}
