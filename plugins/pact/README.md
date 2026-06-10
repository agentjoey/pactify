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
