package orchestrate

import (
	"testing"

	"github.com/agentjoey/pactify/internal/testenv"
)

func TestProjectFixtureDoesNotStartGitMaintenance(t *testing.T) {
	testenv.AssertNoAutoMaintenance(t, func() { newProject(t) })
}
