package e2e

import (
	"os"
	"testing"

	"github.com/agentjoey/pactify/internal/testenv"
)

// These tests start the pactify CLI. When they run inside a delegated job, the
// inherited job marker would trip the nested-delegation guard.
func TestMain(m *testing.M) {
	os.Unsetenv("PACTIFY_JOB")
	os.Exit(testenv.Isolate(m))
}
