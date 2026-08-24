package doctor

import (
	"os"
	"testing"

	"github.com/agentjoey/pactify/internal/testenv"
)

// TestMain clears an INHERITED PACT_DIR before any test runs. See
// internal/testenv.Isolate — this package's tests call pact.Init and fold real
// ledgers, so without it a test run launched by orchestrate (which injects an
// absolute PACT_DIR) writes into the REAL repository's .pact.
func TestMain(m *testing.M) {
	restore := testenv.Isolate()
	code := m.Run()
	restore()
	os.Exit(code)
}
