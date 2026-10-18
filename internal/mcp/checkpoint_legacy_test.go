package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// assignT1 assigns t1 to the opencode seat and joins it, leaving one dirty file
// so a checkpoint has something to commit.
func assignT1(t *testing.T, dir string) {
	t.Helper()
	if err := pact.Assign("t1", "f", "feat/x", "opencode", "claude-opus", ".pact/tasks/t1.md", nil); err != nil {
		t.Fatalf("assign: %v", err)
	}
	t.Setenv("PACT_AGENT_ID", "opencode")
	if err := pact.Join("opencode", "worker"); err != nil {
		t.Fatalf("join: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "impl.txt"), []byte("c"), 0o644)
}

func writeStatusJSON(t *testing.T, dir, body string) {
	t.Helper()
	p := filepath.Join(dir, ".pact", "orchestrate", "status.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func callTool(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) resultEnv {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s transport error: %v", name, err)
	}
	return parseResult(t, res)
}

// Runtime files left by the removed orchestrator cannot block pact checkpoints.
func TestCheckpointToolIgnoresLegacyRunStatus(t *testing.T) {
	dir := newRepo(t)
	assignT1(t, dir)
	writeStatusJSON(t, dir, `{"task":"another-task","done":false,"escalated":false,"updated_at":"2099-01-01T00:00:00Z"}`)
	env := callTool(t, connect(t), "checkpoint", map[string]any{"task": "t1", "evidence": "ok"})
	if !env.OK {
		t.Fatalf("legacy status must not block checkpoint: %s", env.Error)
	}
}
