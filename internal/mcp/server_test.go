package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newRepo creates a temp git repo with PACT_DIR set + chdir'd, pact-initialized.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0o644)
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "base"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		c.CombinedOutput()
	}
	t.Setenv("PACT_DIR", filepath.Join(dir, ".pact"))
	t.Chdir(dir)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	if err := pact.Init("p", []string{"claude-opus:orchestrator,reviewer:CLAUDE.md", "opencode:worker:AGENTS.md"}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// connect wires an in-memory client to a fresh pactify MCP server.
func connect(t *testing.T) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	server := New()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// toolText extracts the concatenated text content of a tool result.
func toolText(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestStatusTool(t *testing.T) {
	newRepo(t)
	cs := connect(t)
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "status", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("status errored: %s", toolText(res))
	}
	if !strings.Contains(toolText(res), "project: p") {
		t.Fatalf("status text: %q", toolText(res))
	}
}

func callOK(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s transport error: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s tool error: %s", name, toolText(res))
	}
	return toolText(res)
}

func callErr(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s transport error: %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("%s should have errored, got: %s", name, toolText(res))
	}
	return toolText(res)
}

func TestFullLifecycleViaMCP(t *testing.T) {
	newRepo(t)
	cs := connect(t)
	callOK(t, cs, "assign", map[string]any{"task": "T1", "feature": "F", "branch": "feat/x", "owner": "opencode", "reviewer": "claude-opus", "spec": ".pact/tasks/T1.md"})

	t.Setenv("PACT_AGENT_ID", "opencode")
	callOK(t, cs, "join", map[string]any{"seat": "opencode", "roles": "worker"})
	os.WriteFile("impl.txt", []byte("code"), 0o644)
	callOK(t, cs, "checkpoint", map[string]any{"task": "T1", "evidence": "tests green"})

	callErr(t, cs, "accept", map[string]any{"task": "T1"})

	t.Setenv("PACT_AGENT_ID", "claude-opus")
	callOK(t, cs, "accept", map[string]any{"task": "T1"})
	callOK(t, cs, "merge", map[string]any{"feature": "F"})
	if !strings.Contains(callOK(t, cs, "status", nil), "status: shipped") {
		t.Fatal("feature not shipped")
	}
	callOK(t, cs, "validate", nil)
}

func TestToolsFailClosedWithoutAgentID(t *testing.T) {
	newRepo(t)
	cs := connect(t)
	t.Setenv("PACT_AGENT_ID", "")
	callErr(t, cs, "assign", map[string]any{"task": "T9", "feature": "F", "branch": "b", "owner": "opencode", "reviewer": "claude-opus"})
}

func TestMergeRule2ViaMCP(t *testing.T) {
	newRepo(t)
	cs := connect(t)
	callOK(t, cs, "assign", map[string]any{"task": "T1", "feature": "F", "branch": "b", "owner": "opencode", "reviewer": "claude-opus"})
	callErr(t, cs, "merge", map[string]any{"feature": "F"})
}

func TestResources(t *testing.T) {
	newRepo(t)
	cs := connect(t)
	st, err := cs.ReadResource(context.Background(), &sdk.ReadResourceParams{URI: "pact://state"})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Contents) != 1 || !strings.Contains(st.Contents[0].Text, "project: p") {
		t.Fatalf("state resource: %+v", st.Contents)
	}
	lg, err := cs.ReadResource(context.Background(), &sdk.ReadResourceParams{URI: "pact://log"})
	if err != nil {
		t.Fatal(err)
	}
	if len(lg.Contents) != 1 || !strings.Contains(lg.Contents[0].Text, `"event_type":"init"`) {
		t.Fatalf("log resource: %+v", lg.Contents)
	}
}
