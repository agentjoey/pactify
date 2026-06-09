package projection

import "testing"

func TestRenderByteAligned(t *testing.T) {
	ev := "PASS\tok"
	st := State{
		Project: "p",
		Agents:  []Seat{{ID: "claude-opus", Roles: []string{"orchestrator", "reviewer"}}, {ID: "opencode", Roles: []string{"worker"}}},
		Features: []Feature{{ID: "F", Branch: "feat/x", Status: "shipped", Tasks: []Task{
			{ID: "T1", Owner: "opencode", Status: "accepted", Reviewer: "claude-opus", Spec: ".pact/tasks/T1.md", Evidence: &ev},
		}}},
	}
	want := "project: p\n" +
		"working_tree_holder: null\n" +
		"agents:\n" +
		"  - { id: claude-opus, roles: [orchestrator, reviewer] }\n" +
		"  - { id: opencode, roles: [worker] }\n" +
		"features:\n" +
		"  - id: F\n" +
		"    branch: feat/x\n" +
		"    status: shipped\n" +
		"    tasks:\n" +
		"      - id: T1\n" +
		"        owner: opencode\n" +
		"        status: accepted\n" +
		"        reviewer: claude-opus\n" +
		"        spec: .pact/tasks/T1.md\n" +
		"        evidence: PASS ok\n"
	if got := Render(st); got != want {
		t.Fatalf("render mismatch:\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}

func TestRenderNullEvidenceAndEmptyFeatures(t *testing.T) {
	st := State{Project: "p", Agents: []Seat{{ID: "a", Roles: []string{"worker"}}}}
	want := "project: p\nworking_tree_holder: null\nagents:\n  - { id: a, roles: [worker] }\nfeatures:\n"
	if got := Render(st); got != want {
		t.Fatalf("empty-features render mismatch:\n%q\nwant\n%q", got, want)
	}
}
