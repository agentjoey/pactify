package planner_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/agent"
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
	for _, field := range []string{`"feature"`, `"branch"`, `"tasks"`, `"id"`, `"owner"`, `"reviewer"`, `"spec"`, `"verify"`, `"deps"`, `"dimension"`, `"tier"`} {
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

// exec-tiering-parse: the prompt must instruct the planner to tier every task
// and to dual-write it (manifest `tier` field + spec `tier:` line).
func TestPromptInstructsTiers(t *testing.T) {
	out := planner.BuildPrompt(samplePromptInput())

	for _, want := range []string{"L0", "L1", "L2", "L3", "tier:"} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing tier guidance %q", want)
		}
	}
	if !strings.Contains(out, "`tier`") {
		t.Error("prompt does not require the manifest `tier` field")
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

// P3: the prompt must hand the planner the role catalog + each seat's bound
// role, and instruct it to name a role type per task — that is how a task's
// nature ("frontend") routes to the seat configured for it.
func TestPromptCarriesRoleCatalogAndRoutingRule(t *testing.T) {
	p := planner.BuildPrompt(planner.PromptInput{
		Goal: "g", Feature: "f", RepoTree: "x",
		Seats: []planner.SeatInfo{
			{ID: "w2", Roles: []string{"worker"}, Drivable: true, Role: "frontend", Model: "claude-opus-4-8", Kind: "claude-code"},
			{ID: "w3", Roles: []string{"worker"}, Drivable: true},
		},
		RoleCatalog: []planner.RoleInfo{
			{Name: "frontend", Kind: "claude-code", Model: "claude-opus-4-8", BoundSeats: []string{"w2"}},
			{Name: "test", Kind: "opencode", Model: "deepseek/deepseek-v4-flash"},
		},
	})
	for _, want := range []string{
		"frontend",         // catalog entry
		"claude-opus-4-8",  // the model behind the role
		"w2",               // the bound seat
		"no seat is bound", // the unbound-role warning wording
		"role",             // the per-task routing instruction
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q:\n%s", want, p)
		}
	}
}

// A project with NO role bound (RoleCatalog empty, the default zero value of
// PromptInput) must render byte-for-byte as it did before role routing
// existed: no Role catalog / Role routing section at all. This is the hard
// regression requirement agy-roles must not break — reusing samplePromptInput
// (which never sets RoleCatalog) proves it directly against the same fixture
// every other prompt test already exercises.
func TestPromptOmitsRoleCatalogWhenEmpty(t *testing.T) {
	out := planner.BuildPrompt(samplePromptInput())
	for _, unwanted := range []string{"## Role catalog", "## Role routing"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("prompt with no RoleCatalog must omit %q:\n%s", unwanted, out)
		}
	}
}

// Once antigravity is bound to a lightweight-work role (agy-roles, using the
// exact catalog docs/operations.md recommends), the planner's existing
// role-catalog rendering must surface its kind and model like any other
// role — proving the routing mechanism (already covered by
// TestPromptCarriesRoleCatalogAndRoutingRule) works unmodified for the new
// kind rather than requiring bespoke prompt-rendering support.
func TestPromptCarriesAntigravityRoleCatalog(t *testing.T) {
	// The fixture must be built from REAL values rather than plausible-looking
	// strings: the kind has to be registered and both models have to come from
	// antigravity's curated RunnerProfile.Models (read through
	// agent.CandidateModels — never duplicated here). Otherwise this test would
	// go on passing over a kind or a model that no longer exists.
	const kind = "antigravity"
	if _, ok := agent.Get(kind); !ok {
		t.Fatalf("%s is not a registered agent kind (%v)", kind, agent.Kinds())
	}
	models := agent.CandidateModels(kind)
	boundModel, unboundModel := "gemini-3.7-flash-medium", "gemini-3.7-flash-low"
	for _, m := range []string{boundModel, unboundModel} {
		if !slices.Contains(models, m) {
			t.Fatalf("model %q is not in %s's curated models %v", m, kind, models)
		}
	}

	p := planner.BuildPrompt(planner.PromptInput{
		Goal: "g", Feature: "f", RepoTree: "x",
		Seats: []planner.SeatInfo{
			{ID: "agy-1", Roles: []string{"worker"}, Drivable: true, Role: "frontend", Kind: kind, Model: boundModel},
		},
		RoleCatalog: []planner.RoleInfo{
			{Name: "frontend", Kind: kind, Model: boundModel, BoundSeats: []string{"agy-1"}},
			{Name: "ops", Kind: kind, Model: unboundModel},
		},
	})
	for _, want := range []string{
		"## Role catalog",
		"frontend",                          // bound role name
		kind,                                // the kind behind it
		boundModel,                          // the bound role's model
		"agy-1",                             // the bound seat
		"ops",                               // unbound role still listed
		unboundModel,                        // unbound role's model
		"no seat is bound to this role yet", // unbound-role catalog wording
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q:\n%s", want, p)
		}
	}
}
