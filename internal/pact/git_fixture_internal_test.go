package pact

import (
	"testing"

	"github.com/agentjoey/pactify/internal/testenv"
)

func TestRepoFixtureDoesNotStartGitMaintenance(t *testing.T) {
	testenv.AssertNoAutoMaintenance(t, func() { newRepo(t) })
}
