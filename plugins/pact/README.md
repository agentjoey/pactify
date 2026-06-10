# pact (Claude Code plugin)

Adds the **pact** skill and the **pact** MCP server (every protocol verb as a tool) for
multi-agent coordination.

## Prerequisite
The `pactify` binary must be on your PATH (the MCP server runs `pactify mcp`):

```bash
curl -fsSL https://raw.githubusercontent.com/agentjoey/pactify/main/install.sh | sh
pactify setup
```

This plugin's SessionStart hook reminds you if `pactify` is missing.

## Note on double wiring
If you also wire a project-level server via `pactify setup` / `pactify agent add claude-code`
(which writes a seat-baked `.mcp.json` in your repo), you'll have two `pact` MCP servers in
that repo — prefer one: keep the project wiring for seat identity, or rely on this plugin
and launch Claude Code with `PACT_AGENT_ID` exported.
