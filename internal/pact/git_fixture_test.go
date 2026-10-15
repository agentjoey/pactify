package pact_test

import (
	"testing"

	"github.com/agentjoey/pactify/internal/testenv"
)

func TestGitRepoFixtureDoesNotStartGitMaintenance(t *testing.T) {
	testenv.AssertNoAutoMaintenance(t, func() { newGitRepo(t) })
}
