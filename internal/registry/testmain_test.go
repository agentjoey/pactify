package registry

import (
	"os"
	"testing"

	"github.com/agentjoey/pactify/internal/testenv"
)

// TestMain clears an INHERITED PACT_DIR before any test runs. See
// internal/testenv.Isolate — without it, this package's tests write into
// whatever repository the launching process pointed PACT_DIR at, which for an
// orchestrated agent is the real one.
func TestMain(m *testing.M) {
	restore := testenv.Isolate()
	code := m.Run()
	restore()
	os.Exit(code)
}
