package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

func TestPathCheckDetectsDir(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "pactify")
	if c := checkPath(exe, dir+":/usr/bin"); !c.OK {
		t.Fatalf("expected PATH check ok when exe dir is on PATH: %+v", c)
	}
	if c := checkPath(exe, "/usr/bin:/bin"); c.OK {
		t.Fatalf("expected PATH check to fail when exe dir absent: %+v", c)
	}
}

func TestSeatCheck(t *testing.T) {
	if c := checkSeat(""); c.OK {
		t.Fatal("empty PACT_AGENT_ID should not be ok")
	}
	if c := checkSeat("opencode"); !c.OK {
		t.Fatal("set PACT_AGENT_ID should be ok")
	}
}

func TestRepoCheckNoPactDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	c := checkRepo(dir)
	if c.OK {
		t.Fatal("missing .pact should not be ok")
	}
	if !strings.Contains(c.Detail, "pactify setup") {
		t.Fatalf("remediation should point at setup: %+v", c)
	}
}

func TestRepoCheckGuardsForeignCwd(t *testing.T) {
	// cwd != process cwd must fail loudly, never validate the wrong repo.
	c := checkRepo(t.TempDir())
	if c.OK || !strings.Contains(c.Detail, "internal") {
		t.Fatalf("foreign cwd should trip the guard: %+v", c)
	}
}

func TestRepoCheckValidRepo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0o644)
	exec.Command("git", "-C", dir, "add", "-A").Run()
	exec.Command("git", "-C", dir, "commit", "-q", "-m", "base").Run()
	t.Setenv("PACT_AGENT_ID", "x")
	if err := pact.Init("p", []string{"x:worker:CLAUDE.md"}); err != nil {
		t.Fatal(err)
	}
	c := checkRepo(dir)
	if !c.OK || !strings.Contains(c.Detail, "conformant") {
		t.Fatalf("valid repo should pass: %+v", c)
	}
}

func TestAgentWiringCheckDetectsConfigs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{"mcp":{"pact":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := checkAgentWiring(dir)
	if !c.OK || !strings.Contains(c.Detail, "opencode.json") {
		t.Fatalf("expected opencode.json detected: %+v", c)
	}
	if c2 := checkAgentWiring(t.TempDir()); c2.OK {
		t.Fatal("empty dir should report no wiring")
	}
}

// A pre-existing markdown entry with no pact managed-block must NOT count as
// wiring — the check is content-aware, not mere file presence.
func TestAgentWiringCheckIgnoresUnwiredMarkdown(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# my notes\nno pact here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := checkAgentWiring(dir); c.OK {
		t.Fatalf("CLAUDE.md without the pact:begin marker should not count as wiring: %+v", c)
	}
}

// A legacy config that pins a seat id must be flagged with a re-wire hint
// (spec seat-identity §3.4). A de-identified config passes.
func TestPinnedIdentityCheckFlagsLegacyWiring(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".mcp.json"),
		[]byte(`{"mcpServers":{"pact":{"command":"pactify","args":["mcp"],"env":{"PACT_AGENT_ID":"lead"}}}}`), 0o644)
	c := checkPinnedIdentity(dir)
	if c.OK {
		t.Fatalf("pinned identity must be flagged (not OK): %+v", c)
	}
	if !strings.Contains(c.Detail, "lead") || !strings.Contains(c.Detail, "agent add") {
		t.Fatalf("hint must name the seat and the re-wire fix: %+v", c)
	}
}

func TestPinnedIdentityCheckPassesWhenClean(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".mcp.json"),
		[]byte(`{"mcpServers":{"pact":{"command":"pactify","args":["mcp"],"env":{"PACT_AGENT_ID":"${PACT_AGENT_ID:-}"}}}}`), 0o644)
	if c := checkPinnedIdentity(dir); !c.OK {
		t.Fatalf("de-identified config must pass: %+v", c)
	}
}
