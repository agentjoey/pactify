package sub

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The delegation path must not depend on the frozen orchestrator or the
// governance-mode packages (AJ-724), so either can be removed on its own.
// Imports are read with go/parser (non-test files, all build tags) so the
// check stays cheap and spawns no go command.
func TestDelegationPathAvoidsFrozenPackages(t *testing.T) {
	const module = "github.com/agentjoey/pactify/"
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	frozen := map[string]bool{}
	for _, p := range []string{"orchestrate", "audit", "sessions", "agent", "agentcfg", "agentreg", "roles", "pact", "ledger", "projection", "event", "planner", "schedule", "runguard"} {
		frozen[module+"internal/"+p] = true
	}
	seen := map[string]bool{}
	var walk func(path, from string)
	walk = func(path, from string) {
		if seen[path] || !strings.HasPrefix(path, module) {
			return
		}
		seen[path] = true
		if frozen[path] {
			t.Errorf("%s imports frozen package %s", from, path)
			return
		}
		files, err := filepath.Glob(filepath.Join(root, strings.TrimPrefix(path, module), "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no Go files for %s: %v", path, err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				dep, _ := strconv.Unquote(imp.Path.Value)
				walk(dep, path)
			}
		}
	}
	for _, pkg := range []string{"sub", "subpanel", "cockpit"} {
		walk(module+"internal/"+pkg, "root")
	}
	if len(seen) < 4 {
		t.Fatalf("walked only %d packages", len(seen))
	}
}
