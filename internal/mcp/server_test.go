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
