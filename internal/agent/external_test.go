package agent

import "testing"

func TestParseFormatAndScope(t *testing.T) {
	cases := map[string]Format{"mcpServers": JSONMcpServers, "opencode": JSONOpencode, "toml": TOML}
	for s, want := range cases {
		got, ok := ParseFormat(s)
		if !ok || got != want {
			t.Errorf("ParseFormat(%q) = %v,%v want %v", s, got, ok, want)
		}
	}
	if _, ok := ParseFormat("bogus"); ok {
		t.Error("ParseFormat(bogus) should be !ok")
	}
	if sc, ok := ParseScope("global"); !ok || sc != Global {
		t.Errorf("ParseScope(global) = %v,%v", sc, ok)
	}
	if _, ok := ParseScope("nope"); ok {
		t.Error("ParseScope(nope) should be !ok")
	}
}

func TestRegisterExternalAddsKindAndRejectsBuiltinCollision(t *testing.T) {
	t.Cleanup(func() { UnregisterExternal("myagent") })

	rp := RunnerProfile{Command: "myagent", DefaultModel: "m1", Models: []string{"m1"},
		BuildArgs: func(model string, _ PermPosture, briefing string) []string {
			return []string{"run", "-m", model, briefing}
		}}
	ext := External{Kind: "myagent", Entry: "AGENTS.md", Binary: "myagent",
		HasMCP: true, MCPConfigPath: ".myagent/mcp.json", MCPScope: Project, MCPFormat: JSONMcpServers,
		Runner: &rp}
	if err := RegisterExternal(ext); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, ok := Get("myagent"); !ok {
		t.Fatal("myagent not in registry after register")
	}
	if !Drivable("myagent") {
		t.Fatal("myagent should be drivable (has runner)")
	}
	if got := CandidateModels("myagent"); len(got) != 1 || got[0] != "m1" {
		t.Fatalf("CandidateModels = %v", got)
	}
	if err := RegisterExternal(External{Kind: "opencode", Binary: "x"}); err == nil {
		t.Fatal("registering a built-in kind must be rejected")
	}
}
