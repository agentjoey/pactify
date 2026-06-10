package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	c := checkRepo(t.TempDir())
	if c.OK {
		t.Fatal("missing .pact should not be ok")
	}
	if !strings.Contains(c.Detail, "pactify setup") {
		t.Fatalf("remediation should point at setup: %+v", c)
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
