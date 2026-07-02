package orchestrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// detectDefaultGate infers a build+test gate from the project's manifest/lockfiles;
// a pnpm lockfile beats a bare package.json, and Go is the conservative fallback.
func TestDetectDefaultGate(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  string
	}{
		{"pnpm", []string{"pnpm-lock.yaml", "package.json"}, "pnpm build && pnpm test"},
		{"npm", []string{"package.json"}, "npm run build && npm test"},
		{"cargo", []string{"Cargo.toml"}, "cargo build && cargo test"},
		{"go", []string{"go.mod"}, fallbackGate},
		{"none", []string{"README.md"}, fallbackGate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := detectDefaultGate(dir); got != tc.want {
				t.Fatalf("detectDefaultGate = %q, want %q", got, tc.want)
			}
		})
	}
}

// resolveGate prefers an explicit `pactify config gate` over the inferred default.
func TestResolveGatePrefersConfig(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	dir := newProject(t) // Go repo → would default to fallbackGate

	custom := "pnpm build && pnpm typecheck && pnpm lint && pnpm format:check && pnpm test"
	if err := pact.At(dir).As("orch").ConfigGate(custom); err != nil {
		t.Fatal(err)
	}
	if got := resolveGate(dir); got != custom {
		t.Fatalf("resolveGate = %q, want the configured gate %q", got, custom)
	}
}

// With no per-task verify line, gateCommands uses the resolved project gate
// (configured here) rather than the hardcoded Go fallback.
func TestGateCommandsUsesProjectGate(t *testing.T) {
	t.Setenv("PACT_DIR", "")
	t.Setenv("PACT_AGENT_ID", "orch")
	dir := newProject(t)
	writeSpec(t, dir, "t1", "") // spec with no verify: line
	assign(t, dir, "t1", "f", "feat/x", filepath.Join(".pact", "tasks", "t1.md"))

	custom := "pnpm build && pnpm test"
	if err := pact.At(dir).As("orch").ConfigGate(custom); err != nil {
		t.Fatal(err)
	}
	st, _ := pact.At(dir).StateProjection()
	for _, f := range st.Features {
		if f.ID != "f" {
			continue
		}
		cmds := gateCommands(dir, f)
		if len(cmds) != 1 || cmds[0] != custom {
			t.Fatalf("gateCommands = %v, want [%q]", cmds, custom)
		}
		return
	}
	t.Fatal("feature F not found")
}
