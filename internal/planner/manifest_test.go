package planner_test

import (
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/planner"
)

var validJSON = []byte(`{
  "feature": "demo-feature",
  "branch": "feat-demo",
  "tasks": [
    {
      "id": "t1",
      "owner": "alice",
      "reviewer": "bob",
      "spec": ".pact/tasks/t1.md",
      "verify": "go test ./...",
      "deps": []
    },
    {
      "id": "t2",
      "owner": "bob",
      "reviewer": "alice",
      "spec": ".pact/tasks/t2.md",
      "verify": "go test ./...",
      "deps": ["t1"]
    }
  ]
}`)

func TestManifest(t *testing.T) {
	TestParseRoundTrip(t)
	TestParseMalformedJSON(t)
	TestParseMissingRequiredFields(t)
	TestValidateValidPlan(t)
	TestValidateEmptyFeature(t)
	TestValidateEmptyBranch(t)
	TestValidateMissingTaskID(t)
	TestValidateMissingOwner(t)
	TestValidateMissingReviewer(t)
	TestValidateMissingSpec(t)
	TestValidateMissingVerify(t)
	TestValidateUnknownOwner(t)
	TestValidateUnknownReviewer(t)
	TestValidateOwnerEqualsReviewer(t)
	TestValidateDepNotExist(t)
	TestValidateDepSelfReferencing(t)
	TestValidateDepCycle(t)
	TestValidateDuplicateTaskID(t)
	TestValidateMultipleErrorsAggregated(t)
}

func TestParseRoundTrip(t *testing.T) {
	p, err := planner.Parse(validJSON)
	if err != nil {
		t.Fatal(err)
	}
	if p.Feature != "demo-feature" {
		t.Fatalf("Feature = %q", p.Feature)
	}
	if p.Branch != "feat-demo" {
		t.Fatalf("Branch = %q", p.Branch)
	}
	if len(p.Tasks) != 2 {
		t.Fatalf("len Tasks = %d", len(p.Tasks))
	}
	if p.Tasks[0].ID != "t1" {
		t.Fatalf("T0 ID = %q", p.Tasks[0].ID)
	}
	if len(p.Tasks[1].Deps) != 1 || p.Tasks[1].Deps[0] != "t1" {
		t.Fatalf("T1 Deps = %v", p.Tasks[1].Deps)
	}
}

func TestParseMalformedJSON(t *testing.T) {
	_, err := planner.Parse([]byte(`{bad`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestParseMissingRequiredFields(t *testing.T) {
	_, err := planner.Parse([]byte(`{"feature":"F"}`))
	if err == nil {
		t.Fatal("expected error for missing required fields")
	}
}

func TestValidateValidPlan(t *testing.T) {
	p, err := planner.Parse(validJSON)
	if err != nil {
		t.Fatal(err)
	}
	roster := []string{"alice", "bob"}
	if err := p.Validate(roster); err != nil {
		t.Fatalf("valid plan must validate: %v", err)
	}
}

func TestValidateEmptyFeature(t *testing.T) {
	p := planner.Plan{Branch: "feat-x", Tasks: []planner.PlanTask{
		{ID: "t1", Owner: "a", Reviewer: "b", Spec: "s", Verify: "v"},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "feature") {
		t.Fatalf("want feature error, got %v", err)
	}
}

func TestValidateEmptyBranch(t *testing.T) {
	p := planner.Plan{Feature: "feat-x", Tasks: []planner.PlanTask{
		{ID: "t1", Owner: "a", Reviewer: "b", Spec: "s", Verify: "v"},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "branch") {
		t.Fatalf("want branch error, got %v", err)
	}
}

func TestValidateMissingTaskID(t *testing.T) {
	p := planner.Plan{Feature: "feat-x", Branch: "b", Tasks: []planner.PlanTask{
		{Owner: "a", Reviewer: "b", Spec: "s", Verify: "v"},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("want id error, got %v", err)
	}
}

func TestValidateMissingOwner(t *testing.T) {
	p := planner.Plan{Feature: "feat-x", Branch: "b", Tasks: []planner.PlanTask{
		{ID: "t1", Reviewer: "b", Spec: "s", Verify: "v"},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("want owner error, got %v", err)
	}
}

func TestValidateMissingReviewer(t *testing.T) {
	p := planner.Plan{Feature: "feat-x", Branch: "b", Tasks: []planner.PlanTask{
		{ID: "t1", Owner: "a", Spec: "s", Verify: "v"},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "reviewer") {
		t.Fatalf("want reviewer error, got %v", err)
	}
}

func TestValidateMissingSpec(t *testing.T) {
	p := planner.Plan{Feature: "feat-x", Branch: "b", Tasks: []planner.PlanTask{
		{ID: "t1", Owner: "a", Reviewer: "b", Verify: "v"},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "spec") {
		t.Fatalf("want spec error, got %v", err)
	}
}

func TestValidateMissingVerify(t *testing.T) {
	p := planner.Plan{Feature: "feat-x", Branch: "b", Tasks: []planner.PlanTask{
		{ID: "t1", Owner: "a", Reviewer: "b", Spec: "s"},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "verify") {
		t.Fatalf("want verify error, got %v", err)
	}
}

func TestValidateUnknownOwner(t *testing.T) {
	p := planner.Plan{Feature: "feat-x", Branch: "b", Tasks: []planner.PlanTask{
		{ID: "t1", Owner: "nobody", Reviewer: "b", Spec: "s", Verify: "v"},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "nobody") {
		t.Fatalf("want unknown owner error, got %v", err)
	}
}

func TestValidateUnknownReviewer(t *testing.T) {
	p := planner.Plan{Feature: "feat-x", Branch: "b", Tasks: []planner.PlanTask{
		{ID: "t1", Owner: "a", Reviewer: "nobody", Spec: "s", Verify: "v"},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "nobody") {
		t.Fatalf("want unknown reviewer error, got %v", err)
	}
}

func TestValidateOwnerEqualsReviewer(t *testing.T) {
	p := planner.Plan{Feature: "feat-x", Branch: "b", Tasks: []planner.PlanTask{
		{ID: "t1", Owner: "a", Reviewer: "a", Spec: "s", Verify: "v"},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "owner") || !strings.Contains(err.Error(), "reviewer") {
		t.Fatalf("want owner==reviewer error, got %v", err)
	}
}

func TestValidateDepNotExist(t *testing.T) {
	p := planner.Plan{Feature: "feat-x", Branch: "b", Tasks: []planner.PlanTask{
		{ID: "t1", Owner: "a", Reviewer: "b", Spec: "s", Verify: "v", Deps: []string{"t99"}},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "t99") || !strings.Contains(err.Error(), "dep") {
		t.Fatalf("want unknown dep error, got %v", err)
	}
}

func TestValidateDepSelfReferencing(t *testing.T) {
	p := planner.Plan{Feature: "feat-x", Branch: "b", Tasks: []planner.PlanTask{
		{ID: "t1", Owner: "a", Reviewer: "b", Spec: "s", Verify: "v", Deps: []string{"t1"}},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "self") {
		t.Fatalf("want self-dep error, got %v", err)
	}
}

func TestValidateDepCycle(t *testing.T) {
	p := planner.Plan{Feature: "feat-x", Branch: "b", Tasks: []planner.PlanTask{
		{ID: "t1", Owner: "a", Reviewer: "b", Spec: "s", Verify: "v", Deps: []string{"t2"}},
		{ID: "t2", Owner: "b", Reviewer: "a", Spec: "s", Verify: "v", Deps: []string{"t1"}},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("want cycle error, got %v", err)
	}
}

func TestValidateDuplicateTaskID(t *testing.T) {
	p := planner.Plan{Feature: "feat-x", Branch: "b", Tasks: []planner.PlanTask{
		{ID: "t1", Owner: "a", Reviewer: "b", Spec: "s", Verify: "v"},
		{ID: "t1", Owner: "b", Reviewer: "a", Spec: "s", Verify: "v"},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate id error, got %v", err)
	}
}

func TestValidateMultipleErrorsAggregated(t *testing.T) {
	p := planner.Plan{Feature: "", Branch: "", Tasks: []planner.PlanTask{
		{ID: "t1", Owner: "a", Reviewer: "a", Spec: "s", Verify: "v"},
	}}
	err := p.Validate([]string{"a", "b"})
	if err == nil {
		t.Fatal("expected aggregated errors")
	}
	s := err.Error()
	if strings.Count(s, "\n") < 2 {
		t.Fatalf("want at least 3 aggregated errors, got: %s", s)
	}
}

func TestValidateRejectsNonSlugFeature(t *testing.T) {
	js := []byte(`{"feature":"Add_2FA","branch":"feat-2fa","tasks":[
		{"id":"add-2fa-otp","owner":"alice","reviewer":"bob","spec":".pact/tasks/x.md","verify":"go test ./..."}]}`)
	p, err := planner.Parse(js)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := p.Validate([]string{"alice", "bob"}); err == nil {
		t.Fatal("non-slug feature id must be rejected")
	}
}

func TestValidateRejectsNonSlugTaskID(t *testing.T) {
	js := []byte(`{"feature":"add-2fa","branch":"feat-2fa","tasks":[
		{"id":"OTP_Gen","owner":"alice","reviewer":"bob","spec":".pact/tasks/x.md","verify":"go test ./..."}]}`)
	p, _ := planner.Parse(js)
	if err := p.Validate([]string{"alice", "bob"}); err == nil {
		t.Fatal("non-slug task id must be rejected")
	}
}

func TestValidateRejectsOverlongFeature(t *testing.T) {
	long := "a-" + strings.Repeat("x", 45) // > 40 chars
	js := []byte(`{"feature":"` + long + `","branch":"feat-x","tasks":[
		{"id":"t1","owner":"alice","reviewer":"bob","spec":".pact/tasks/x.md","verify":"go test ./..."}]}`)
	p, _ := planner.Parse(js)
	if err := p.Validate([]string{"alice", "bob"}); err == nil {
		t.Fatal("feature id over 40 chars must be rejected")
	}
}

func TestValidateAcceptsSlugNames(t *testing.T) {
	js := []byte(`{"feature":"add-2fa-login","branch":"feat-2fa","tasks":[
		{"id":"add-2fa-otp-gen","owner":"alice","reviewer":"bob","spec":".pact/tasks/x.md","verify":"go test ./..."}]}`)
	p, _ := planner.Parse(js)
	if err := p.Validate([]string{"alice", "bob"}); err != nil {
		t.Fatalf("valid slug plan must pass: %v", err)
	}
}

func TestValidSlug(t *testing.T) {
	for _, ok := range []string{"add-2fa", "feat1", "a"} {
		if !planner.ValidSlug(ok) {
			t.Errorf("ValidSlug(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "Add_2FA", "-x", "a b"} {
		if planner.ValidSlug(bad) {
			t.Errorf("ValidSlug(%q) = true, want false", bad)
		}
	}
}
