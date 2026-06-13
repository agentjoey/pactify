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

func TestSuggest_NonDrivableMarked(t *testing.T) {
	got := Suggest([]string{"claude-code", "antigravity"})
	var anti Binding
	for _, b := range got {
		if b.Kind == "antigravity" {
			anti = b
		}
	}
	if anti.Drivable {
		t.Error("antigravity should be marked non-drivable")
	}
	if anti.Roles[0] != "worker" {
		t.Errorf("antigravity roles = %v, want worker", anti.Roles)
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
