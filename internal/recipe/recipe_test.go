package recipe

import (
	"strings"
	"testing"
)

func TestNames(t *testing.T) {
	names := Names()
	if len(names) != 3 {
		t.Fatalf("Names() len = %d, want 3", len(names))
	}
	want := []string{"add-tests", "review-harden", "spec-to-plan"}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("Names()[%d] = %q, want %q", i, n, want[i])
		}
	}
}

func TestGet(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"add-tests exists", "add-tests", true},
		{"review-harden exists", "review-harden", true},
		{"spec-to-plan exists", "spec-to-plan", true},
		{"nope missing", "nope", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := Get(tt.input)
			if ok != tt.want {
				t.Errorf("Get(%q) ok = %v, want %v", tt.input, ok, tt.want)
			}
		})
	}
}

func TestGetReturnsRecipeWithTasks(t *testing.T) {
	r, ok := Get("add-tests")
	if !ok {
		t.Fatal("Get(add-tests) returned false")
	}
	if r.Name != "add-tests" {
		t.Errorf("Name = %q, want add-tests", r.Name)
	}
	if len(r.Tasks) != 1 {
		t.Fatalf("Tasks len = %d, want 1", len(r.Tasks))
	}
	if r.Tasks[0].ID != "t-tests" {
		t.Errorf("Tasks[0].ID = %q, want t-tests", r.Tasks[0].ID)
	}
	if !strings.Contains(r.Tasks[0].SpecTemplate, "{{goal}}") {
		t.Error("SpecTemplate should contain {{goal}} token")
	}
}

func TestExpand(t *testing.T) {
	tests := []struct {
		name      string
		recipe    string
		goal      string
		wantTasks int
		wantErr   bool
		wantMsg   string
	}{
		{
			name:      "add-tests single task",
			recipe:    "add-tests",
			goal:      "write unit tests for Foo",
			wantTasks: 1,
		},
		{
			name:      "review-harden two tasks",
			recipe:    "review-harden",
			goal:      "add caching layer",
			wantTasks: 2,
		},
		{
			name:      "spec-to-plan two tasks",
			recipe:    "spec-to-plan",
			goal:      "build user auth module",
			wantTasks: 2,
		},
		{
			name:    "empty goal",
			recipe:  "add-tests",
			goal:    "",
			wantErr: true,
			wantMsg: "recipe: goal is required",
		},
		{
			name:    "whitespace-only goal",
			recipe:  "add-tests",
			goal:    "   ",
			wantErr: true,
			wantMsg: "recipe: goal is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, ok := Get(tt.recipe)
			if !ok {
				t.Fatalf("Get(%q) returned false", tt.recipe)
			}
			tasks, err := r.Expand(tt.goal)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if err.Error() != tt.wantMsg {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tasks) != tt.wantTasks {
				t.Errorf("expanded tasks len = %d, want %d", len(tasks), tt.wantTasks)
			}
		})
	}
}

func TestExpandSubstitutesGoal(t *testing.T) {
	r, ok := Get("add-tests")
	if !ok {
		t.Fatal("Get returned false")
	}
	goal := "我的目标"
	tasks, err := r.Expand(goal)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tasks[0].Spec, goal) {
		t.Errorf("Spec = %q, want containing %q", tasks[0].Spec, goal)
	}
	if strings.Contains(tasks[0].Spec, "{{goal}}") {
		t.Error("Spec should not contain raw {{goal}} token after expansion")
	}
}

func TestExpandPreservesDeps(t *testing.T) {
	r, ok := Get("review-harden")
	if !ok {
		t.Fatal("Get returned false")
	}
	tasks, err := r.Expand("add caching")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks len = %d, want 2", len(tasks))
	}
	if tasks[0].ID != "t-impl" {
		t.Errorf("tasks[0].ID = %q, want t-impl", tasks[0].ID)
	}
	if len(tasks[0].Deps) != 0 {
		t.Errorf("tasks[0].Deps len = %d, want 0", len(tasks[0].Deps))
	}
	if tasks[1].ID != "t-harden" {
		t.Errorf("tasks[1].ID = %q, want t-harden", tasks[1].ID)
	}
	if len(tasks[1].Deps) != 1 || tasks[1].Deps[0] != "t-impl" {
		t.Errorf("tasks[1].Deps = %v, want [t-impl]", tasks[1].Deps)
	}
}

func TestExpandDepsAreIndependentSlices(t *testing.T) {
	r, ok := Get("review-harden")
	if !ok {
		t.Fatal("Get returned false")
	}
	tasks, err := r.Expand("add caching")
	if err != nil {
		t.Fatal(err)
	}
	deps := tasks[1].Deps
	if len(deps) != 1 || deps[0] != "t-impl" {
		t.Fatalf("unexpected deps: %v", deps)
	}
	deps[0] = "mutated"
	tasks2, err := r.Expand("another goal")
	if err != nil {
		t.Fatal(err)
	}
	if tasks2[1].Deps[0] != "t-impl" {
		t.Error("ExpandedTask.Deps must be independent from the recipe template; mutation leaked")
	}
}
