package codexschema

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestSchemaDrift reports when the vendored schema no longer matches the codex
// CLI installed on this machine.
//
// It needs the codex binary, so it cannot run in ordinary CI. That is why it is
// only half of the warning system: TestSchemaContract (no binary required) is
// the part that gates every build, and this test is the staleness alarm that
// runs wherever codex is actually installed — a developer machine, or the
// scheduled .github/workflows/codex-schema.yml job.
//
// Absence of codex is a skip so `go test ./...` stays green on machines without
// it. Set PACTIFY_REQUIRE_CODEX=1 to turn that skip into a failure; CI jobs that
// install codex on purpose set it, so a failed install cannot masquerade as a
// passing drift check.
func TestSchemaDrift(t *testing.T) {
	vendoredVersion := readVendoredVersion(t)

	if _, err := exec.LookPath("codex"); err != nil {
		msg := "codex is not on PATH, so the vendored schema was NOT checked for drift. " +
			"The vendored schema is pinned to codex " + vendoredVersion + "; if the codex CLI has " +
			"moved on, internal/cockpit/codex.go may be mapping a protocol that no longer exists. " +
			"Install codex and re-run, or rely on the scheduled codex-schema workflow."
		if truthy(os.Getenv(requireCodexEnv)) {
			t.Fatalf("%s=1 but %s", requireCodexEnv, msg)
		}
		t.Skip("SKIPPED (not a pass): " + msg)
	}

	if installed := codexVersion(t); installed != "" && installed != vendoredVersion {
		t.Errorf("codex CLI is %s but %s pins the vendored schema to %s%s",
			installed, versionFile, vendoredVersion, regenHint)
	}

	tmpDir := t.TempDir()
	cmd := exec.Command("codex", "app-server", "generate-json-schema", "--out", tmpDir)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("generate-json-schema failed: %v", err)
	}

	generated, err := os.ReadFile(filepath.Join(tmpDir, vendoredSchema))
	if err != nil {
		t.Fatalf("read generated schema: %v", err)
	}
	vendored, err := os.ReadFile(vendoredSchema)
	if err != nil {
		t.Fatalf("read vendored schema %s: %v", vendoredSchema, err)
	}

	var g, v map[string]any
	if err := json.Unmarshal(generated, &g); err != nil {
		t.Fatalf("unmarshal generated schema: %v", err)
	}
	if err := json.Unmarshal(vendored, &v); err != nil {
		t.Fatalf("unmarshal vendored schema: %v", err)
	}

	if !reflect.DeepEqual(g, v) {
		t.Fatalf("codex app-server schema drifted from vendored %s:\n%s%s",
			vendoredSchema, strings.Join(summarizeDiff(v, g), "\n"), regenHint)
	}
}

func readVendoredVersion(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(versionFile)
	if err != nil {
		t.Fatalf("read %s: %v", versionFile, err)
	}
	v := strings.TrimSpace(string(raw))
	if v == "" {
		t.Fatalf("%s is empty; it must record the codex version the schema came from", versionFile)
	}
	return v
}

// codexVersion returns the bare version from `codex --version` (which prints
// e.g. "codex-cli 0.144.4"), or "" if it cannot be determined.
func codexVersion(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("codex", "--version").Output()
	if err != nil {
		t.Logf("codex --version failed: %v (version comparison skipped)", err)
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "0", "false", "no":
		return false
	}
	return true
}
