//go:build acpsmoke

// Package smoke test: a real ACP stint against a locally-installed, logged-in
// vendor CLI. Excluded from the normal build (needs a real agent + credentials);
// run with: go test -tags acpsmoke ./internal/orchestrate/ -run TestACPSmoke
// Skips cleanly when the kimi binary isn't on PATH.
package orchestrate

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestACPSmoke(t *testing.T) {
	if _, err := exec.LookPath("kimi"); err != nil {
		t.Skip("kimi not on PATH — skipping real ACP smoke")
	}
	dir := t.TempDir()
	r := NewAcpRunner(30*time.Second, PermissionAuto)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// A minimal stint: the agent just needs to start a session and take one turn.
	err := r.Run(ctx, LaunchContext{
		Seat: "smoke", Kind: "kimi-cli", Task: "smoke",
		Briefing: "Reply with the single word OK and stop.", RepoDir: dir,
	})
	if err != nil {
		t.Fatalf("ACP smoke stint failed: %v", err)
	}
}
