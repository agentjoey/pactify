package doctor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindRepoRootFindsBridgeFromChild(t *testing.T) {
	root := t.TempDir()
	bridgeDir := filepath.Join(root, "bridge", "claude-host")
	if err := os.MkdirAll(bridgeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bridgeDir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	child := filepath.Join(root, "cloud", "relay")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}

	found, ok := FindRepoRoot(child)
	if !ok {
		t.Fatal("expected FindRepoRoot to find bridge from child dir")
	}
	if found != root {
		t.Fatalf("expected repo root %q, got %q", root, found)
	}
}

func TestFindRepoRootStopsAtRoot(t *testing.T) {
	root := t.TempDir()
	// No bridge/claude-host/package.json here.
	found, ok := FindRepoRoot(root)
	if ok {
		t.Fatalf("expected no repo root, got %q", found)
	}
}

func TestBridgeChecksDepsPresent(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "bridge", "claude-host", "node_modules", "@anthropic-ai", "claude-agent-sdk")
	if err := os.MkdirAll(marker, 0o755); err != nil {
		t.Fatal(err)
	}

	checks := BridgeChecks(root)
	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}

	nodeCheck := checks[0]
	if nodeCheck.Name != "cli node: present" {
		t.Fatalf("unexpected check name: %q", nodeCheck.Name)
	}
	// The node check follows the host PATH; we only assert it reports sensibly.
	if nodeCheck.OK && !strings.Contains(nodeCheck.Detail, string(os.PathSeparator)) {
		t.Fatalf("expected node path detail when node is present, got %q", nodeCheck.Detail)
	}

	depsCheck := checks[1]
	if depsCheck.Name != "claude bridge: deps" {
		t.Fatalf("unexpected check name: %q", depsCheck.Name)
	}
	if !depsCheck.OK {
		t.Fatalf("expected deps ok when marker dir exists: %s", depsCheck.Detail)
	}
	if !strings.Contains(depsCheck.Detail, "materialized") {
		t.Fatalf("expected materialized detail, got %q", depsCheck.Detail)
	}
}

func TestBridgeChecksDepsMissing(t *testing.T) {
	root := t.TempDir()
	// Intentionally no node_modules marker.
	checks := BridgeChecks(root)
	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}

	depsCheck := checks[1]
	if depsCheck.Name != "claude bridge: deps" {
		t.Fatalf("unexpected check name: %q", depsCheck.Name)
	}
	if depsCheck.OK {
		t.Fatal("expected deps !ok when marker dir missing")
	}
	if !strings.Contains(depsCheck.Detail, "--setup-bridge") {
		t.Fatalf("expected setup-bridge remediation, got %q", depsCheck.Detail)
	}
}

func TestSetupBridgeMissingDir(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	err := SetupBridge(root, &out)
	if err == nil {
		t.Fatal("expected error when bridge directory is missing")
	}
	if !strings.Contains(err.Error(), "bridge directory not found") {
		t.Fatalf("expected clear missing-directory error, got %q", err.Error())
	}
}
