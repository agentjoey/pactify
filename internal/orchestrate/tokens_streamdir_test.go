package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/acp"
	"github.com/agentjoey/pactify/internal/tokens"
)

// 2026-07-19 orchestrate e2e F6: in a sandbox run lc.RepoDir is the THROWAWAY
// worktree, so the token store (.pact/orchestrate/tokens.json) was written
// there and destroyed at teardown — every vendor's TOK silently vanished
// (dashboard TOK=0, /stats empty). Token records belong with the other
// teardown-surviving runtime artifacts (status/streams/escalation): the
// StreamDir. Both transports share recordTaskTokens, so each gets a test.

func TestAcpUsageTokensLandInStreamDirNotSandbox(t *testing.T) {
	mainDir, sbDir := t.TempDir(), t.TempDir()
	fc := newFakeAcpConn()
	fc.prompt = func(fc *fakeAcpConn) (acp.StopReason, error) {
		fc.emitUpdate(acp.SessionUpdate{Kind: "usage", Usage: &acp.Usage{InputTokens: 100, OutputTokens: 40}})
		return "end_turn", nil
	}
	r := NewAcpRunner(0, PermissionAuto)
	r.Spawn = captureSpawn(fc, nil)
	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Task: "t-acp", Kind: "kimi-cli", RepoDir: sbDir, StreamDir: mainDir}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := tokens.Load(mainDir).Get("t-acp"); got != 140 {
		t.Fatalf("ACP usage must land in the StreamDir (survives sandbox teardown), got %d want 140", got)
	}
	if _, err := os.Stat(filepath.Join(sbDir, ".pact", "orchestrate", "tokens.json")); !os.IsNotExist(err) {
		t.Fatalf("no token store may be written into the throwaway sandbox dir")
	}
}

func TestCmdTokensLandInStreamDirNotSandbox(t *testing.T) {
	mainDir, sbDir := t.TempDir(), t.TempDir()
	lc := LaunchContext{Seat: "w", Task: "t-cmd", Kind: "opencode", RepoDir: sbDir, StreamDir: mainDir}
	recordTokens(lc, lc.Kind, `{"usage":{"input_tokens":30,"output_tokens":12}}`)
	if got := tokens.Load(mainDir).Get("t-cmd"); got != 42 {
		t.Fatalf("cmd-transport tokens must land in the StreamDir, got %d want 42", got)
	}
	if _, err := os.Stat(filepath.Join(sbDir, ".pact", "orchestrate", "tokens.json")); !os.IsNotExist(err) {
		t.Fatalf("no token store may be written into the throwaway sandbox dir")
	}
}
