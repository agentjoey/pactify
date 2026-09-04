package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// The MCP tool is the path an agent (and a human driving one) takes, so it
// needs the same guard as the CLI: checkpoint commits the whole worktree.
func TestCheckpointToolRefusedWhileAnotherTaskIsBeingDriven(t *testing.T) {
	dir := newRepo(t)
	assignT1(t, dir)
	writeStatusJSON(t, dir, `{"feature":"m4","task":"m4-s11","seat":"kimi-worker","done":false,"escalated":false,"updated_at":"`+time.Now().UTC().Format(time.RFC3339)+`"}`)

	env := callTool(t, connect(t), "checkpoint", map[string]any{"task": "t1", "evidence": "ok"})

	if env.OK {
		t.Fatalf("checkpoint tool must refuse while another task is driven, got ok: %s", env.Data)
	}
	if !strings.Contains(env.Error, "m4-s11") {
		t.Errorf("error must name the running task, got: %s", env.Error)
	}
	log, _ := os.ReadFile(filepath.Join(dir, ".pact", "log.jsonl"))
	if strings.Contains(string(log), `"checkpoint"`) {
		t.Errorf("refused checkpoint must not reach the ledger:\n%s", log)
	}
}

// The worker's own task must stay open — the briefing tells workers to
// checkpoint through exactly this tool.
func TestCheckpointToolAllowsTheDrivenTask(t *testing.T) {
	dir := newRepo(t)
	assignT1(t, dir)
	writeStatusJSON(t, dir, `{"feature":"f","task":"t1","seat":"opencode","done":false,"escalated":false,"updated_at":"`+time.Now().UTC().Format(time.RFC3339)+`"}`)

	env := callTool(t, connect(t), "checkpoint", map[string]any{"task": "t1", "evidence": "ok"})

	if !env.OK {
		t.Fatalf("worker checkpoint of the driven task must pass, got error: %s", env.Error)
	}
}
