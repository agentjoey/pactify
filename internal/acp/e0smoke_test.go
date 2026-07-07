package acp

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestE0AcpSmoke drives the REAL pinned claude ACP bridge to answer the cockpit
// spec's E0 go/no-go questions. Gated by E0_SMOKE=1 (spawns the bridge, needs
// network + claude Agent SDK auth), so it never runs in CI.
//
//	# via the resolved bin (WORKS):
//	B=$(ls -d ~/.npm/_npx/*/node_modules/@agentclientprotocol/claude-agent-acp/dist/index.js)
//	E0_SMOKE=1 E0_ACP_CMD="node $B" go test ./internal/acp/ -run TestE0AcpSmoke -v -timeout 200s
//
// E0 FINDINGS (2026-07-07, this harness):
//   - ⑤ HANDSHAKE: `npx -y @…acp@0.57.0` (the runner's current acpCommand form)
//     NEVER completes initialize — ALL npx forms (-y / --no-install / bare) hang
//     the stdio JSON-RPC pipe. Invoking the resolved bin directly
//     (node <path>/dist/index.js) returns initialize instantly. → the runner MUST
//     resolve+invoke the bin, not shell out to npx. (RED flag for the ACP path.)
//   - ② LoadSession: bridge advertises loadSession:true + sessionCapabilities.resume
//     — cross-restart resume is protocol-supported.
//   - newSession needs claude Agent SDK auth: with no creds it fails
//     `-32603 spawn Unknown system error -88`. acpEnv STRIPS ANTHROPIC_API_KEY for
//     claude (assuming Claude Code OAuth) — likely wrong for this Agent-SDK bridge.
//     Full round-trip (newSession→prompt) needs auth to validate.
func TestE0AcpSmoke(t *testing.T) {
	if os.Getenv("E0_SMOKE") != "1" {
		t.Skip("set E0_SMOKE=1 to run the real ACP bridge smoke")
	}
	dir := t.TempDir()
	// Command is overridable via E0_ACP_CMD (space-separated) so we can compare
	// `npx -y …` (the runner's current form) against invoking the resolved bin
	// directly (node <path>/dist/index.js). Default: npx form.
	cmd, args := "npx", []string{"-y", "@agentclientprotocol/claude-agent-acp@0.57.0"}
	if override := os.Getenv("E0_ACP_CMD"); override != "" {
		parts := strings.Fields(override)
		cmd, args = parts[0], parts[1:]
	}
	// strip ANTHROPIC_API_KEY so the bridge uses interactive OAuth creds (mirrors acpEnv).
	env := []string{"ANTHROPIC_API_KEY="}

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	c, err := Spawn(ctx, cmd, args, env, dir)
	if err != nil {
		t.Fatalf("E0: spawn bridge: %v", err)
	}
	defer c.Close()

	var mu chan struct{} = make(chan struct{}, 1)
	var texts []string
	c.OnSessionUpdate(func(u SessionUpdate) {
		if s := extractText(u); s != "" {
			texts = append(texts, s)
		}
		select {
		case mu <- struct{}{}:
		default:
		}
	})

	init, err := c.Initialize(ctx)
	if err != nil {
		t.Fatalf("E0-⑤: initialize failed: %v", err)
	}
	t.Logf("E0-⑤ initialize OK · loadSession capability = %v", init.LoadSession)

	sid, err := c.NewSession(ctx, dir)
	if err != nil {
		t.Fatalf("E0-⑤: newSession failed: %v", err)
	}
	t.Logf("E0-⑤ newSession OK · id=%s", sid)

	stop, err := c.Prompt(ctx, sid, "Reply with exactly the word: PONG. Do not use any tools.")
	if err != nil {
		t.Fatalf("E0-⑤: prompt failed: %v", err)
	}
	joined := strings.Join(texts, " ")
	t.Logf("E0-⑤ prompt round-trip OK · stopReason=%s · reply=%q", stop, truncate(joined, 200))
	if !strings.Contains(strings.ToUpper(joined), "PONG") {
		t.Logf("E0-⑤ NOTE: reply did not contain PONG (agent nondeterminism, not necessarily a bridge failure)")
	}

	// E0-② LoadSession: verify the capability, then try to re-attach the session
	// from a FRESH client (the cross-restart recovery primitive).
	if !init.LoadSession {
		t.Logf("E0-② RESULT: bridge does NOT advertise loadSession capability → cross-restart resume unsupported by this bridge")
		return
	}
	c2, err := Spawn(ctx, cmd, args, env, dir)
	if err != nil {
		t.Fatalf("E0-②: spawn 2nd bridge: %v", err)
	}
	defer c2.Close()
	if _, err := c2.Initialize(ctx); err != nil {
		t.Fatalf("E0-②: 2nd initialize: %v", err)
	}
	if err := c2.LoadSession(ctx, sid); err != nil {
		t.Logf("E0-② RESULT: LoadSession(%s) on a fresh client FAILED: %v", sid, err)
	} else {
		t.Logf("E0-② RESULT: LoadSession(%s) on a fresh client SUCCEEDED — cross-restart resume viable", sid)
	}
}

func extractText(u SessionUpdate) string {
	// Best-effort: pull any "text" field out of the raw update.
	raw := string(u.Raw)
	i := strings.Index(raw, `"text":"`)
	if i < 0 {
		return ""
	}
	rest := raw[i+len(`"text":"`):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
