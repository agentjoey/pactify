package e2e

import (
	"os"
	"testing"

	"github.com/agentjoey/pactify/internal/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.Isolate(m)) }
