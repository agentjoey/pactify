package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serve is multi-project: every handler is handed a specific project's root and
// must read THAT project's ledger. paths.Log/paths.LogIn are process-scoped —
// when PACT_DIR is an absolute path they return it verbatim and IGNORE the base
// they were given (paths.DirIn). That behavior is correct for the engine and the
// orchestrate driver, which are pinned to one repo on purpose; inside serve it
// silently redirects a read to whatever repo PACT_DIR names, no matter which
// project the caller asked about.
//
// So the rule for this package is: ledger paths come from the project root the
// caller was given, never from process env. This test pins the rule, because a
// single misuse is invisible in review and in every test that neutralizes
// PACT_DIR (which testenv.Isolate does for the whole suite).
func TestServeNeverResolvesLedgerPathsFromProcessEnv(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	var offenders []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			code := line
			if idx := strings.Index(code, "//"); idx >= 0 {
				code = code[:idx] // ignore prose in comments
			}
			if strings.Contains(code, "paths.LogIn(") || strings.Contains(code, "paths.Log()") ||
				strings.Contains(code, "paths.StateIn(") || strings.Contains(code, "paths.State()") {
				offenders = append(offenders, filepath.Join(".", name)+":"+itoa(i+1)+"  "+strings.TrimSpace(line))
			}
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("serve must resolve ledger/state paths from the project root it was given, not from PACT_DIR.\n"+
			"Use ledger.Path(projectRoot) instead of paths.LogIn/paths.Log:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
