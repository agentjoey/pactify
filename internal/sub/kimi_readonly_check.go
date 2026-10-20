package sub

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/cockpit"
)

// CheckKimiReadonly verifies only this job's recorded private session store.
func (s Store) CheckKimiReadonly(id string, result Result, env []string) error {
	st, err := s.Load(id)
	if err != nil {
		return err
	}
	if st.Request.Harness != "kimi" || st.Request.Access != "read-only" || st.EffectiveConfigMode() != "minimal" {
		return fmt.Errorf("Read-only verification unavailable: expected a private kimi read-only session")
	}
	// Diagnostic/E2E callers may read a job started with a different PACTIFY_HOME.
	// Match that launch environment, as the opencode policy checker does.
	expected := ""
	for _, entry := range env {
		if home, ok := strings.CutPrefix(entry, "PACTIFY_HOME="); ok && home != "" {
			expected, err = filepath.Abs(filepath.Join(home, "harness-homes", "kimi-readonly"))
		}
	}
	if expected == "" && err == nil {
		expected, err = managedKimiReadOnlyHome()
	}
	if err != nil {
		return fmt.Errorf("Read-only verification unavailable: cannot locate kimi read-only home")
	}
	info, err := os.Lstat(expected)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("Read-only verification unavailable: kimi read-only home is missing or redirected")
	}
	expected, err = filepath.EvalSymlinks(expected)
	actual, actualErr := filepath.EvalSymlinks(st.HarnessHome)
	if err != nil || actualErr != nil || actual != expected {
		return fmt.Errorf("Read-only verification unavailable: job home is not the managed kimi-readonly home")
	}
	return cockpit.CheckKimiReadonly(expected, result.SessionID)
}
