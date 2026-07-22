# pact (Claude Code plugin)

Adds the **pact** skill and the **pact** MCP server (every protocol verb as a tool) for
multi-agent coordination.

## Prerequisite
The `pactify` binary must be on your PATH (the MCP server runs `pactify mcp`):

```bash
curl -fsSL https://pactify.dev/install.sh | sh
pactify setup
```

This plugin's SessionStart hook reminds you if `pactify` is missing.

## Note on double wiring
If you also wire a project-level server via `pactify setup` / `pactify agent add claude-code`
(which writes a `.mcp.json` in your repo), you'll have two `pact` MCP servers in that repo —
prefer one. Either way, seat identity is the same: resolved from `PACT_AGENT_ID` else the
untracked `.pact/seat` file (`pactify seat use <id>`), never pinned in the config.
