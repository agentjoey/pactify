#!/usr/bin/env bats

REPO="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

@test "marketplace.json is valid and its plugin source exists" {
  run python3 -c "import json; m=json.load(open('$REPO/.claude-plugin/marketplace.json')); assert m['name']=='pactify'; assert any(p['name']=='pact' for p in m['plugins']); print([p['source'] for p in m['plugins']][0])"
  [ "$status" -eq 0 ]
  src="$(printf '%s' "$output" | tail -1)"
  [ -d "$REPO/${src#./}" ]
  [ -f "$REPO/${src#./}/.claude-plugin/plugin.json" ]
}

@test "plugin .mcp.json registers the pact server with bare pactify command" {
  run python3 -c "import json; m=json.load(open('$REPO/plugins/pact/.mcp.json')); s=m['mcpServers']['pact']; assert s['command']=='pactify'; assert s['args']==['mcp']; print('ok')"
  [ "$status" -eq 0 ]; [ "$output" = "ok" ]
}
