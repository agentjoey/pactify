package mcp

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/registry"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// A session launched in one repo can drive a DIFFERENT registered project by name,
// breaking the MCP server's launch-cwd binding (spec coordination-authority P2):
// `status` defaults to the launch dir but `status project=beta` reads beta's board.
func TestProjectAddressingByName(t *testing.T) {
	// Isolate the machine registry to a temp home; named addressing must root at the
	// project path (so PACT_DIR must NOT be set to a global absolute override).
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "claude-opus")

	repoA := initNamedRepo(t, "alpha") // the session's launch dir
	repoB := initNamedRepo(t, "beta")  // addressed by name

	reg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add("beta", repoB, ""); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}

	t.Chdir(repoA)
	cs := connect(t)

	// Default (no project) → the launch dir, alpha.
	if got := callOK(t, cs, "status", map[string]any{}); !strings.Contains(got, "project: alpha") {
		t.Fatalf("default status should read the launch dir (alpha): %q", got)
	}
	// Addressed by name → beta, though the session is bound to repoA.
	if got := callOK(t, cs, "status", map[string]any{"project": "beta"}); !strings.Contains(got, "project: beta") {
		t.Fatalf("project-addressed status should read beta: %q", got)
	}
	// Unknown name fails with a discoverable list.
	res, _ := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "status", Arguments: map[string]any{"project": "nope"}})
	if !res.IsError || !strings.Contains(toolText(res), "beta") {
		t.Fatalf("unknown project should error and list known names: %q", toolText(res))
	}
	// The projects tool advertises what can be addressed.
	if got := callOK(t, cs, "projects", map[string]any{}); !strings.Contains(got, "beta") {
		t.Fatalf("projects tool should list beta: %q", got)
	}
}

// initNamedRepo makes a temp git repo pact-initialized as project `name`, with no
// PACT_DIR override so .pact resolves under the repo dir (dir-aware path).
func initNamedRepo(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-q", "--allow-empty", "-m", "base"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		c.CombinedOutput()
	}
	if err := pact.At(dir).As("claude-opus").Init(name, []string{
		"claude-opus:orchestrator,reviewer:CLAUDE.md",
		"opencode:worker:AGENTS.md",
	}); err != nil {
		t.Fatalf("init %s: %v", name, err)
	}
	return dir
}
