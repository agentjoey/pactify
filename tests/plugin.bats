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

@test "plugin hooks.json is valid and points at an executable hook script" {
  run python3 -c "import json; h=json.load(open('$REPO/plugins/pact/hooks/hooks.json')); cmd=h['hooks']['SessionStart'][0]['hooks'][0]['command']; assert 'check-pactify.sh' in cmd; print('ok')"
  [ "$status" -eq 0 ]; [ "$output" = "ok" ]
  [ -x "$REPO/plugins/pact/hooks/check-pactify.sh" ]
}

@test "hook exits 0 whether pactify is present or missing (never breaks session start)" {
  run sh "$REPO/plugins/pact/hooks/check-pactify.sh"
  [ "$status" -eq 0 ]
  run env PATH=/usr/bin:/bin sh "$REPO/plugins/pact/hooks/check-pactify.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"install.sh | sh"* ]]
}
