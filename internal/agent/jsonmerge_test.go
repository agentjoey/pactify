package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, p string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("invalid json written: %v\n%s", err, b)
	}
	return m
}

func TestMergeMcpServersPreservesAndIsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".mcp.json")
	os.WriteFile(p, []byte(`{"mcpServers":{"other":{"command":"x"}},"unknownTop":1}`), 0o644)
	inv := Invoke{Command: "pactify", Args: []string{"mcp"}, Env: map[string]string{"PACT_AGENT_ID": "claude-code"}}

	if err := mergeServer(p, JSONMcpServers, inv); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(p)
	m := read(t, p)
	servers := m["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatal("existing server 'other' was dropped")
	}
	if m["unknownTop"] == nil {
		t.Fatal("unknown top-level key was dropped")
	}
	pact := servers["pact"].(map[string]any)
	if pact["command"] != "pactify" || pact["env"].(map[string]any)["PACT_AGENT_ID"] != "claude-code" {
		t.Fatalf("pact entry wrong: %v", pact)
	}

	if err := mergeServer(p, JSONMcpServers, inv); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(p)
	if string(first) != string(second) {
		t.Fatalf("merge not byte-idempotent:\n%s\n---\n%s", first, second)
	}
}

func TestMergeOpencodeDialect(t *testing.T) {
	p := filepath.Join(t.TempDir(), "opencode.json")
	inv := Invoke{Command: "pactify", Args: []string{"mcp"}, Env: map[string]string{"PACT_AGENT_ID": "opencode"}}
	if err := mergeServer(p, JSONOpencode, inv); err != nil {
		t.Fatal(err)
	}
	m := read(t, p)
	pact := m["mcp"].(map[string]any)["pact"].(map[string]any)
	if pact["type"] != "local" || pact["enabled"] != true {
		t.Fatalf("opencode entry missing type/enabled: %v", pact)
	}
	cmd := pact["command"].([]any)
	if cmd[0] != "pactify" || cmd[1] != "mcp" {
		t.Fatalf("opencode command array wrong: %v", cmd)
	}
	if pact["environment"].(map[string]any)["PACT_AGENT_ID"] != "opencode" {
		t.Fatalf("opencode environment wrong: %v", pact)
	}
}

func TestMergeGlobalCreatesParentDir(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "deep", "config.json")
	inv := Invoke{Command: "pactify", Args: []string{"mcp", "--project", "/r"}, Env: map[string]string{"PACT_AGENT_ID": "antigravity"}}
	if err := mergeServer(p, JSONMcpServers, inv); err != nil {
		t.Fatalf("merge should mkdir -p the parent: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}

func TestMergeInvalidJSONErrorsWithoutClobber(t *testing.T) {
	p := filepath.Join(t.TempDir(), "broken.json")
	os.WriteFile(p, []byte(`{not json`), 0o644)
	inv := Invoke{Command: "pactify", Args: []string{"mcp"}, Env: map[string]string{"PACT_AGENT_ID": "x"}}
	if err := mergeServer(p, JSONMcpServers, inv); err == nil {
		t.Fatal("expected error on invalid existing JSON")
	}
	b, _ := os.ReadFile(p)
	if string(b) != `{not json` {
		t.Fatalf("invalid file was clobbered: %s", b)
	}
}

func TestMergeSymlinkSafe(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	os.WriteFile(real, []byte(`{}`), 0o644)
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlink unsupported")
	}
	inv := Invoke{Command: "pactify", Args: []string{"mcp"}, Env: map[string]string{"PACT_AGENT_ID": "x"}}
	if err := mergeServer(link, JSONMcpServers, inv); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Lstat(link)
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("symlink was followed/preserved; expected replacement with a regular file")
	}
}

func TestMergeOverwritesExistingPact(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".mcp.json")
	os.WriteFile(p, []byte(`{"mcpServers":{"pact":{"command":"OLD"}}}`), 0o644)
	inv := Invoke{Command: "pactify", Args: []string{"mcp"}, Env: map[string]string{"PACT_AGENT_ID": "claude-code"}}
	if err := mergeServer(p, JSONMcpServers, inv); err != nil {
		t.Fatal(err)
	}
	pact := read(t, p)["mcpServers"].(map[string]any)["pact"].(map[string]any)
	if pact["command"] != "pactify" {
		t.Fatalf("stale pact entry not overwritten: %v", pact)
	}
}

func TestMergeNonObjectParentErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".mcp.json")
	os.WriteFile(p, []byte(`{"mcpServers":"oops"}`), 0o644)
	inv := Invoke{Command: "pactify", Args: []string{"mcp"}, Env: map[string]string{"PACT_AGENT_ID": "x"}}
	if err := mergeServer(p, JSONMcpServers, inv); err == nil {
		t.Fatal("expected error when parent key is a non-object")
	}
	b, _ := os.ReadFile(p)
	if string(b) != `{"mcpServers":"oops"}` {
		t.Fatalf("file was clobbered: %s", b)
	}
}

func TestSnippetTOMLFormat(t *testing.T) {
	inv := Invoke{Command: "pactify", Args: []string{"mcp", "--project", "/r"}, Env: map[string]string{"PACT_AGENT_ID": "codex"}}
	got := snippet(TOML, inv)
	want := "[mcp_servers.pact]\ncommand = \"pactify\"\nargs = [\"mcp\", \"--project\", \"/r\"]\nenv = { PACT_AGENT_ID = \"codex\" }\n"
	if got != want {
		t.Fatalf("snippet TOML mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSnippetJSONOpencode(t *testing.T) {
	inv := Invoke{Command: "pactify", Args: []string{"mcp"}, Env: map[string]string{"PACT_AGENT_ID": "opencode"}}
	got := snippet(JSONOpencode, inv)
	if !strings.Contains(got, `"type": "local"`) || !strings.Contains(got, `"mcp"`) {
		t.Fatalf("opencode snippet wrong:\n%s", got)
	}
}
