package finish

import (
	"strings"
	"testing"
)

func TestPush_RequiresRemoteAndBranch(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		branch string
	}{
		{"empty remote", "", "main"},
		{"empty branch", "origin", ""},
		{"both empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := Finisher{Run: func(dir, name string, args ...string) (string, error) {
				t.Fatal("Run should not be called when remote/branch are empty")
				return "", nil
			}}
			_, err := f.Push("/repo", tc.remote, tc.branch)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestPush_CallsGitPush(t *testing.T) {
	var gotDir, gotName string
	var gotArgs []string
	f := Finisher{Run: func(dir, name string, args ...string) (string, error) {
		gotDir = dir
		gotName = name
		gotArgs = args
		return "pushed ok", nil
	}}
	out, err := f.Push("/repo", "origin", "main")
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != "/repo" {
		t.Fatalf("got dir %q, want %q", gotDir, "/repo")
	}
	if gotName != "git" {
		t.Fatalf("got name %q, want %q", gotName, "git")
	}
	if len(gotArgs) != 3 || gotArgs[0] != "push" || gotArgs[1] != "origin" || gotArgs[2] != "main" {
		t.Fatalf("got args %v, want [push origin main]", gotArgs)
	}
	if out != "pushed ok" {
		t.Fatalf("got output %q, want %q", out, "pushed ok")
	}
}

func TestOpenPR_RequiresBaseHeadTitle(t *testing.T) {
	cases := []struct {
		name  string
		base  string
		head  string
		title string
	}{
		{"empty base", "", "feat/x", "Add X"},
		{"empty head", "main", "", "Add X"},
		{"empty title", "main", "feat/x", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := Finisher{Run: func(dir, name string, args ...string) (string, error) {
				t.Fatal("Run should not be called when params are empty")
				return "", nil
			}}
			_, err := f.OpenPR("/repo", tc.base, tc.head, tc.title, "body")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestOpenPR_NoGH_ReturnsManualCommand(t *testing.T) {
	f := Finisher{
		HasGH: func() bool { return false },
		Run: func(dir, name string, args ...string) (string, error) {
			t.Fatal("Run should not be called when gh is not available")
			return "", nil
		},
	}
	out, err := f.OpenPR("/repo", "main", "feat/x", "Add X", "the body")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "gh pr create") {
		t.Fatalf("expected manual gh pr create command, got %q", out)
	}
	if !strings.Contains(out, "gh not found") {
		t.Fatalf("expected 'gh not found' message, got %q", out)
	}
}

func TestOpenPR_WithGH_CallsGhPrCreate(t *testing.T) {
	var gotDir, gotName string
	var gotArgs []string
	f := Finisher{
		HasGH: func() bool { return true },
		Run: func(dir, name string, args ...string) (string, error) {
			gotDir = dir
			gotName = name
			gotArgs = args
			return "https://github.com/owner/repo/pull/1", nil
		},
	}
	out, err := f.OpenPR("/repo", "main", "feat/x", "Add X", "the body")
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != "/repo" {
		t.Fatalf("got dir %q, want %q", gotDir, "/repo")
	}
	if gotName != "gh" {
		t.Fatalf("got name %q, want %q", gotName, "gh")
	}
	expectedArgs := []string{"pr", "create", "--base", "main", "--head", "feat/x", "--title", "Add X", "--body", "the body"}
	if len(gotArgs) != len(expectedArgs) {
		t.Fatalf("got %d args, want %d args", len(gotArgs), len(expectedArgs))
	}
	for i, a := range gotArgs {
		if a != expectedArgs[i] {
			t.Fatalf("arg[%d]: got %q, want %q", i, a, expectedArgs[i])
		}
	}
	if out != "https://github.com/owner/repo/pull/1" {
		t.Fatalf("got output %q, want pr url", out)
	}
}

func TestOpenPR_HasGHNil_TreatsAsAvailable(t *testing.T) {
	var called bool
	f := Finisher{
		HasGH: nil,
		Run: func(dir, name string, args ...string) (string, error) {
			called = true
			return "pr url", nil
		},
	}
	out, err := f.OpenPR("/repo", "main", "feat/x", "title", "body")
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected Run to be called when HasGH is nil")
	}
	if out != "pr url" {
		t.Fatalf("got %q, want %q", out, "pr url")
	}
}
