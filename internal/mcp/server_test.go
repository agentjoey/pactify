package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	callOK(t, cs, "assign", map[string]any{"task": "t1", "feature": "f", "branch": "feat/x", "owner": "opencode", "reviewer": "claude-opus", "spec": ".pact/tasks/t1.md"})

	t.Setenv("PACT_AGENT_ID", "opencode")
	// join takes no seat arg: the seat is always the session's PACT_AGENT_ID
	if got := callOK(t, cs, "join", map[string]any{"roles": "worker"}); !strings.Contains(got, "joined opencode") {
		t.Fatalf("join did not use PACT_AGENT_ID as the seat: %q", got)
	}
	os.WriteFile("impl.txt", []byte("code"), 0o644)
	callOK(t, cs, "checkpoint", map[string]any{"task": "t1", "evidence": "tests green"})

	callErr(t, cs, "accept", map[string]any{"task": "t1"})

	t.Setenv("PACT_AGENT_ID", "claude-opus")
	callOK(t, cs, "accept", map[string]any{"task": "t1"})
	callOK(t, cs, "merge", map[string]any{"feature": "f"})
	if !strings.Contains(callOK(t, cs, "status", nil), "status: shipped") {
		t.Fatal("feature not shipped")
	}
	callOK(t, cs, "validate", nil)
}

// TestJoinRecordsSessionClientInfo: the MCP join handler stamps the connecting
// client's initialize clientInfo (name/version) onto the join event's payload.
func TestJoinRecordsSessionClientInfo(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	server := New()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	// The client's Implementation is what the server sees as session clientInfo.
	client := sdk.NewClient(&sdk.Implementation{Name: "testclient", Version: "9"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })

	callOK(t, cs, "assign", map[string]any{"task": "t1", "feature": "f", "branch": "feat/x", "owner": "opencode", "reviewer": "claude-opus", "spec": ".pact/tasks/t1.md"})
	t.Setenv("PACT_AGENT_ID", "opencode")
	callOK(t, cs, "join", map[string]any{"roles": "worker"})

	log, err := os.ReadFile(filepath.Join(dir, ".pact/log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), `"client":{"name":"testclient","version":"9"}`) {
		t.Fatalf("join event must carry session clientInfo; log:\n%s", log)
	}
	// Provenance never reaches STATE.yml.
	state, err := os.ReadFile(filepath.Join(dir, ".pact/STATE.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(state), "testclient") || strings.Contains(string(state), "client") {
		t.Fatalf("STATE.yml must not contain client provenance:\n%s", state)
	}
}

func TestToolsFailClosedWithoutAgentID(t *testing.T) {
	newRepo(t)
	cs := connect(t)
	t.Setenv("PACT_AGENT_ID", "")
	callErr(t, cs, "assign", map[string]any{"task": "t9", "feature": "f", "branch": "b", "owner": "opencode", "reviewer": "claude-opus"})
}

func TestMergeRule2ViaMCP(t *testing.T) {
	newRepo(t)
	cs := connect(t)
	callOK(t, cs, "assign", map[string]any{"task": "t1", "feature": "f", "branch": "b", "owner": "opencode", "reviewer": "claude-opus"})
	callErr(t, cs, "merge", map[string]any{"feature": "f"})
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

func TestLogChangeNotifiesSubscribers(t *testing.T) {
	newRepo(t)
	ctx := context.Background()
	got := make(chan string, 4)

	st, ct := sdk.NewInMemoryTransports()
	server := New()
	stop, err := Watch(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, &sdk.ClientOptions{
		ResourceUpdatedHandler: func(_ context.Context, r *sdk.ResourceUpdatedNotificationRequest) {
			got <- r.Params.URI
		},
	})
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if err := cs.Subscribe(ctx, &sdk.SubscribeParams{URI: logURI}); err != nil {
		t.Fatal(err)
	}
	callOK(t, cs, "assign", map[string]any{"task": "t1", "feature": "f", "branch": "b", "owner": "opencode", "reviewer": "claude-opus"})

	select {
	case uri := <-got:
		if uri != logURI {
			t.Fatalf("unexpected uri %q", uri)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no resource-updated notification within 3s")
	}
}
