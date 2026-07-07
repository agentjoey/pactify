package planner_test

import (
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/planner"
)

func samplePromptInput() planner.PromptInput {
	return planner.PromptInput{
		Goal:    "Add a relay client that streams events to a webhook",
		Feature: "relay",
		RepoTree: strings.Join([]string{
			"internal/",
			"  serve/",
			"  planner/",
			"cmd/pactify/",
		}, "\n"),
		Seats: []planner.SeatInfo{
			{ID: "claude", Roles: []string{"orchestrator", "reviewer"}, Drivable: true},
			{ID: "opencode-worker", Roles: []string{"worker"}, Drivable: true},
			{ID: "human-ui", Roles: []string{"worker"}, Drivable: false},
		},
	}
}

func TestPromptContainsGoalFeatureAndRepoTree(t *testing.T) {
	in := samplePromptInput()
	out := planner.BuildPrompt(in)

	for _, want := range []string{in.Goal, in.Feature, "internal/", "serve/", "cmd/pactify/"} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestPromptListsSeatsWithDrivable(t *testing.T) {
	in := samplePromptInput()
	out := planner.BuildPrompt(in)

	for _, s := range in.Seats {
		if !strings.Contains(out, s.ID) {
			t.Errorf("prompt missing seat id %q", s.ID)
		}
		for _, r := range s.Roles {
			if !strings.Contains(out, r) {
				t.Errorf("prompt missing role %q for seat %q", r, s.ID)
			}
		}
	}
	// Drivable state must be visible for both true and false seats.
	if !strings.Contains(out, "human-ui") {
		t.Fatal("prompt missing GUI seat")
	}
	// A non-drivable seat must be flagged so planner avoids it unless necessary.
	low := strings.ToLower(out)
	if !strings.Contains(low, "drivable") {
		t.Error("prompt does not mention Drivable")
	}
}

func TestPromptMentionsTaskAndManifestPaths(t *testing.T) {
	out := planner.BuildPrompt(samplePromptInput())

	for _, want := range []string{".pact/tasks/", ".pact/plan-", "relay-", "plan-relay.json"} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing path fragment %q", want)
		}
	}
}

func TestPromptIncludesManifestSchemaExample(t *testing.T) {
	out := planner.BuildPrompt(samplePromptInput())

	// JSON schema example must show every PlanTask field name.
	for _, field := range []string{`"feature"`, `"branch"`, `"tasks"`, `"id"`, `"owner"`, `"reviewer"`, `"spec"`, `"verify"`, `"deps"`, `"dimension"`} {
		if !strings.Contains(out, field) {
			t.Errorf("manifest schema example missing field %q", field)
		}
	}
}

func TestPromptStatesAssignmentRules(t *testing.T) {
	out := planner.BuildPrompt(samplePromptInput())
	low := strings.ToLower(out)

	if !strings.Contains(low, "owner") || !strings.Contains(low, "reviewer") {
		t.Fatal("prompt missing owner/reviewer assignment guidance")
	}
	// owner≠reviewer must be stated.
	if !strings.Contains(out, "≠") && !strings.Contains(low, "differ") && !strings.Contains(low, "must not be the same") {
		t.Error("prompt does not state owner≠reviewer")
	}
	// Prefer drivable seats.
	if !strings.Contains(low, "prefer") {
		t.Error("prompt does not state preference for drivable seats")
	}
	// Complexity-based allocation.
	if !strings.Contains(low, "complex") {
		t.Error("prompt does not mention complexity-based allocation")
	}
}

func TestPromptRequiresVerifyLine(t *testing.T) {
	out := planner.BuildPrompt(samplePromptInput())
	low := strings.ToLower(out)

	if !strings.Contains(out, "verify:") {
		t.Error("prompt does not require a machine-readable verify: line")
	}
	if !strings.Contains(low, "go test") {
		t.Error("prompt does not give a verify command example")
	}
}

func TestPromptRequiresDependencyChain(t *testing.T) {
	out := planner.BuildPrompt(samplePromptInput())
	low := strings.ToLower(out)

	// Serial dependency chain to avoid single-worktree contention.
	if !strings.Contains(low, "depend") {
		t.Error("prompt does not mention dependency chains")
	}
	if !strings.Contains(low, "worktree") && !strings.Contains(low, "serial") {
		t.Error("prompt does not motivate serial deps / single-worktree avoidance")
	}
}

func TestPromptManifestExampleParses(t *testing.T) {
	out := planner.BuildPrompt(samplePromptInput())

	// Extract the fenced ```json … ``` example and confirm it is a valid Plan.
	const fence = "```json\n"
	i := strings.Index(out, fence)
	if i < 0 {
		t.Fatal("no ```json example block in prompt")
	}
	rest := out[i+len(fence):]
	j := strings.Index(rest, "```")
	if j < 0 {
		t.Fatal("unterminated ```json block")
	}
	if _, err := planner.Parse([]byte(rest[:j])); err != nil {
		t.Errorf("embedded manifest example does not parse: %v", err)
	}
}

func TestPromptDeterministic(t *testing.T) {
	in := samplePromptInput()
	if planner.BuildPrompt(in) != planner.BuildPrompt(in) {
		t.Error("BuildPrompt is not deterministic for identical input")
	}
}

func TestPromptStatesNamingConvention(t *testing.T) {
	out := planner.BuildPrompt(samplePromptInput())
	for _, want := range []string{"kebab-case", "lowercase"} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing naming guidance %q", want)
		}
	}
}

func TestPromptNamingExampleIsSlug(t *testing.T) {
	out := planner.BuildPrompt(samplePromptInput())
	if !strings.Contains(out, "add-2fa-login") {
		t.Error("prompt should show a concrete kebab-case feature id example")
	}
}

func TestPromptStatesVerifyScoping(t *testing.T) {
	out := planner.BuildPrompt(samplePromptInput())
	for _, want := range []string{"Verify rules", "whole-repo", "touched files"} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing verify-scoping guidance %q", want)
		}
	}
}

func TestPromptStatesReviewDimensions(t *testing.T) {
	out := planner.BuildPrompt(samplePromptInput())
	if !strings.Contains(out, "## Review dimensions") {
		t.Error("prompt missing Review dimensions section")
	}
	low := strings.ToLower(out)
	for _, dim := range []string{"correctness", "security", "performance", "maintainability", "ux"} {
		if !strings.Contains(low, dim) {
			t.Errorf("prompt missing dimension %q", dim)
		}
	}
}
