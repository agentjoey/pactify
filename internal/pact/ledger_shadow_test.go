package pact

import (
	"os/exec"
	"testing"

	"github.com/agentjoey/pactify/internal/ledger"
)

// WS-B's contract is "zero behavior change with the flag off". These tests
// exercise the REAL engine append path, not the ledger package in isolation.

func TestEngineDoesNotTouchTheRefWhenFlagIsOff(t *testing.T) {
	t.Setenv("PACTIFY_LEDGER_REF", "")
	dir := initRepoForShadow(t)

	if err := At(dir).As("orch").Join("orch", "orchestrator"); err != nil {
		t.Fatalf("join: %v", err)
	}

	head, err := ledger.RefHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	if head != "" {
		t.Fatal("flag off must leave the ledger ref nonexistent — WS-B is a dark launch")
	}
}

// With the flag on, the two stores must agree EXACTLY. This is the test that
// catches the subtle failure: event_id / ts are generated inside the append, so
// a mirror that re-marshals the caller's struct diverges on every event.
func TestEngineMirrorsByteIdenticalEventsWhenFlagIsOn(t *testing.T) {
	t.Setenv("PACTIFY_LEDGER_REF", "1")
	dir := initRepoForShadow(t)

	// 三次 join：每次都追加一个事件，且 event_id / ts 由 append 内部生成 ——
	// 正是「镜像必须写入逐字节相同内容」要覆盖的场景。
	for i := 0; i < 3; i++ {
		if err := At(dir).As("orch").Join("orch", "orchestrator"); err != nil {
			t.Fatalf("join %d: %v", i, err)
		}
	}

	drift, err := ledger.Verify(dir)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if drift != "" {
		t.Fatalf("file and ref must agree after mirrored appends: %s", drift)
	}
}

func initRepoForShadow(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	t.Setenv("PACT_AGENT_ID", "orch")
	if err := At(dir).Init("p", []string{"orch:orchestrator,reviewer:CLAUDE.md"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	return dir
}
