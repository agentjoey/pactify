package pact

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/event"
)

// newRepo makes a temp git repo, sets PACT_DIR + chdir, returns repo dir.
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
	return dir
}

func TestInitScaffoldsAndWritesInitEvent(t *testing.T) {
	newRepo(t)
	t.Setenv("PACT_AGENT_ID", "claude-opus")
	err := Init("pactify", []string{
		"claude-opus:orchestrator,reviewer:CLAUDE.md",
		"opencode:worker:AGENTS.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(".pact/PROJECT.md"); err != nil {
		t.Fatal("PROJECT.md missing")
	}
	evs, _ := event.ReadAll(".pact/log.jsonl")
	if len(evs) != 1 || evs[0].EventType != "init" {
		t.Fatalf("want 1 init event, got %+v", evs)
	}
	pv, _ := evs[0].Payload["protocol_version"].(float64)
	if int(pv) != 1 {
		t.Fatalf("protocol_version = %v", evs[0].Payload["protocol_version"])
	}
	b, _ := os.ReadFile("AGENTS.md")
	if !strings.Contains(string(b), "PACT_AGENT_ID=opencode") {
		t.Fatal("AGENTS.md not baked")
	}
}

func TestInitFailsClosedWithoutAgentID(t *testing.T) {
	newRepo(t)
	os.Unsetenv("PACT_AGENT_ID")
	if err := Init("p", []string{"a:worker:A.md"}); err == nil {
		t.Fatal("Init must fail closed without PACT_AGENT_ID")
	}
}
