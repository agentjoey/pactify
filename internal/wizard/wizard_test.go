package wizard

import (
	"reflect"
	"testing"
)

func TestSuggest_ClaudeLeadsOthersWork(t *testing.T) {
	got := Suggest([]string{"opencode", "claude-code", "gemini-cli"})
	// claude-code becomes the lead (orchestrator+reviewer); the rest are workers.
	// Order is deterministic: lead first, then workers by kind.
	want := []Binding{
		{Seat: "claude", Kind: "claude-code", Roles: []string{"orchestrator", "reviewer"}, Drivable: true},
		{Seat: "gemini", Kind: "gemini-cli", Roles: []string{"worker"}, Drivable: true},
		{Seat: "opencode", Kind: "opencode", Roles: []string{"worker"}, Drivable: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Suggest =\n%+v\nwant\n%+v", got, want)
	}
}

func TestSuggest_NoClaudeFirstDrivableLeads(t *testing.T) {
	got := Suggest([]string{"opencode", "gemini-cli"})
	if len(got) != 2 {
		t.Fatalf("want 2 bindings, got %d", len(got))
	}
	// No claude → the first drivable kind (alphabetically gemini before opencode)
	// leads.
	if got[0].Kind != "gemini-cli" || !reflect.DeepEqual(got[0].Roles, []string{"orchestrator", "reviewer"}) {
		t.Errorf("lead = %+v, want gemini-cli orchestrator+reviewer", got[0])
	}
	if got[1].Roles[0] != "worker" {
		t.Errorf("second = %+v, want worker", got[1])
	}
}

// Binding.Drivable mirrors agent.Drivable, so this test pins BOTH branches of
// it. antigravity moved to the true branch when it became the agy CLI (agy-kind
// task, 2026-08-22) — this test used to assert the opposite. claude-desktop is
// kept here as the GUI example so the FALSE branch stays covered: without it,
// an agent.Drivable that returned true unconditionally would pass the suite.
func TestSuggest_DrivableAndNonDrivableMarked(t *testing.T) {
	got := Suggest([]string{"claude-code", "antigravity", "claude-desktop"})
	byKind := make(map[string]Binding, len(got))
	for _, b := range got {
		byKind[b.Kind] = b
	}

	anti, ok := byKind["antigravity"]
	if !ok {
		t.Fatalf("antigravity missing from Suggest output: %+v", got)
	}
	if !anti.Drivable {
		t.Error("antigravity (agy) should be marked drivable")
	}
	if anti.Roles[0] != "worker" {
		t.Errorf("antigravity roles = %v, want worker", anti.Roles)
	}

	desk, ok := byKind["claude-desktop"]
	if !ok {
		t.Fatalf("claude-desktop missing from Suggest output: %+v", got)
	}
	if desk.Drivable {
		t.Error("claude-desktop is a GUI kind with no headless runner and must NOT be marked drivable")
	}
	if desk.Roles[0] != "worker" {
		t.Errorf("claude-desktop roles = %v, want worker", desk.Roles)
	}
}

func TestValidate_CoversRoles(t *testing.T) {
	ok := []Binding{
		{Seat: "claude", Roles: []string{"orchestrator", "reviewer"}},
		{Seat: "opencode", Roles: []string{"worker"}},
	}
	if warns := Validate(ok); len(warns) != 0 {
		t.Errorf("valid roster produced warnings: %v", warns)
	}
}

func TestValidate_FlagsMissingWorker(t *testing.T) {
	// Only a lead, no worker → orchestrator would have to do (and review) its own
	// work, breaking separation of duties. Flag it.
	roster := []Binding{{Seat: "claude", Roles: []string{"orchestrator", "reviewer"}}}
	warns := Validate(roster)
	if len(warns) == 0 {
		t.Fatal("expected a warning about no worker seat")
	}
	joined := ""
	for _, w := range warns {
		joined += w
	}
	if !contains(joined, "worker") {
		t.Errorf("warnings %v should mention worker", warns)
	}
}

func TestValidate_FlagsMissingReviewer(t *testing.T) {
	roster := []Binding{
		{Seat: "a", Roles: []string{"orchestrator"}},
		{Seat: "b", Roles: []string{"worker"}},
	}
	warns := Validate(roster)
	if len(warns) == 0 {
		t.Fatal("expected a warning about no reviewer")
	}
}

func TestSuggest_Empty(t *testing.T) {
	if got := Suggest(nil); len(got) != 0 {
		t.Errorf("Suggest(nil) = %v, want empty", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
