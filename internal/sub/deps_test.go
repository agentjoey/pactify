package sub

import (
	"go/build"
	"strings"
	"testing"
)

// The delegation path must not depend on the frozen orchestrator or the
// governance-mode packages (AJ-724), so either can be removed on its own.
func TestDelegationPathAvoidsFrozenPackages(t *testing.T) {
	const module = "github.com/agentjoey/pactify/internal/"
	frozen := map[string]bool{}
	for _, p := range []string{"orchestrate", "audit", "sessions", "agent", "agentcfg", "agentreg", "roles", "pact", "ledger", "projection", "event", "planner", "schedule", "runguard"} {
		frozen[module+p] = true
	}
	seen := map[string]string{}
	var walk func(path, from string)
	walk = func(path, from string) {
		if _, ok := seen[path]; ok || !strings.HasPrefix(path, module) {
			return
		}
		seen[path] = from
		if frozen[path] {
			t.Errorf("%s imports frozen package %s", from, path)
			return
		}
		pkg, err := build.Default.Import(path, ".", 0)
		if err != nil {
			t.Fatalf("import %s: %v", path, err)
		}
		for _, imp := range pkg.Imports {
			walk(imp, path)
		}
	}
	for _, root := range []string{"sub", "subpanel", "cockpit"} {
		walk(module+root, "root")
	}
}
