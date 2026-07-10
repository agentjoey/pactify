package orchestrate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

func TestGate_ExtractVerify_Found(t *testing.T) {
	md := `# Task t1

Some prose describing the task.

verify: go test ./internal/serve/ -run Relay

More prose.
`
	got, ok := extractVerify(md)
	if !ok {
		t.Fatalf("extractVerify: want ok=true, got false")
	}
	if want := "go test ./internal/serve/ -run Relay"; got != want {
		t.Fatalf("extractVerify: got %q, want %q", got, want)
	}
}

func TestGate_ExtractVerify_Missing(t *testing.T) {
	md := `# Task t1

No machine-readable verify line here.
just a regular spec.
`
	got, ok := extractVerify(md)
	if ok {
		t.Fatalf("extractVerify: want ok=false, got true (cmd=%q)", got)
	}
	if got != "" {
		t.Fatalf("extractVerify: want empty cmd, got %q", got)
	}
}

func TestGate_ExtractVerify_QuotedAndPadded(t *testing.T) {
	// Robustness: leading/trailing whitespace and surrounding quotes are trimmed.
	cases := map[string]string{
		`verify:    go build ./...`:           "go build ./...",
		`verify: "go test ./..."`:             "go test ./...",
		`verify: 'go vet ./...'`:              "go vet ./...",
		"   verify:\tgo test -run X   ":       "go test -run X",
		"verify:\"go test ./internal/serve\"": "go test ./internal/serve",
	}
	for line, want := range cases {
		md := "# spec\n" + line + "\nrest\n"
		got, ok := extractVerify(md)
		if !ok {
			t.Errorf("extractVerify(%q): want ok=true, got false", line)
			continue
		}
		if got != want {
			t.Errorf("extractVerify(%q): got %q, want %q", line, got, want)
		}
	}
}

func TestGate_ExtractVerify_FirstWins(t *testing.T) {
	md := "verify: first cmd\nverify: second cmd\n"
	got, ok := extractVerify(md)
	if !ok || got != "first cmd" {
		t.Fatalf("extractVerify: got (%q,%v), want (\"first cmd\",true)", got, ok)
	}
}

// fakeExec is a deterministic test double for cmdExec — no real subprocess.
type fakeExec struct {
	gotDir     string
	gotCommand string
	gotEnv     map[string]string
	calls      int
	exitCode   int
	output     string
	err        error
}

func (f *fakeExec) Run(ctx context.Context, dir, command string, env map[string]string) (int, string, error) {
	f.gotDir = dir
	f.gotCommand = command
	f.gotEnv = env
	f.calls++
	return f.exitCode, f.output, f.err
}

func TestGate_RunGate_Pass(t *testing.T) {
	fe := &fakeExec{exitCode: 0, output: "ok\nPASS\n"}
	ok, detail := runGate(context.Background(), fe, "/repo", "go test ./...", nil)
	if !ok {
		t.Fatalf("runGate: want ok=true, got false (detail=%q)", detail)
	}
	if fe.gotDir != "/repo" || fe.gotCommand != "go test ./..." {
		t.Fatalf("runGate: exec got dir=%q command=%q", fe.gotDir, fe.gotCommand)
	}
}

func TestGate_RunGate_NonZeroFails(t *testing.T) {
	fe := &fakeExec{exitCode: 1, output: "--- FAIL: TestRelay\nboom\n"}
	ok, detail := runGate(context.Background(), fe, "/repo", "go test ./...", nil)
	if ok {
		t.Fatalf("runGate: want ok=false on non-zero exit, got true")
	}
	if !strings.Contains(detail, "boom") {
		t.Fatalf("runGate: detail should include output summary, got %q", detail)
	}
}

func TestGate_RunGate_ExecErrorFails(t *testing.T) {
	fe := &fakeExec{exitCode: -1, output: "", err: errors.New("exec: command not found")}
	ok, detail := runGate(context.Background(), fe, "/repo", "nope", nil)
	if ok {
		t.Fatalf("runGate: want ok=false when exec returns err, got true")
	}
	if !strings.Contains(detail, "command not found") {
		t.Fatalf("runGate: detail should include exec error, got %q", detail)
	}
}

// TestGate_ExtractVerify_RejectsMarkupPrefix (review #2): markdown markup lines
// that merely contain "verify:" after a prefix (blockquote/list/heading) must
// NOT be matched as the verify directive — only a bare `verify:` line is.
func TestGate_ExtractVerify_RejectsMarkupPrefix(t *testing.T) {
	for _, md := range []string{
		"> verify: not a command",
		"- verify: not a command",
		"# verify: not a command",
		"see verify: above",
	} {
		if cmd, ok := extractVerify(md); ok {
			t.Fatalf("markup line %q wrongly matched as verify=%q", md, cmd)
		}
	}
	// A real bare directive still works.
	if cmd, ok := extractVerify("verify: go test ./..."); !ok || cmd != "go test ./..." {
		t.Fatalf("bare verify line: got (%q,%v)", cmd, ok)
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "'plain'"},
		{"with space", "'with space'"},
		{"it's", "'it'\\''s'"},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestChangedFiles(t *testing.T) {
	dir := newProject(t)
	base, err := pact.At(dir).BaseBranch()
	if err != nil {
		t.Fatalf("BaseBranch: %v", err)
	}
	if base == "" {
		t.Fatal("base branch is empty")
	}

	// Modify a file on a feature branch and commit it.
	mustGit(t, dir, []string{"checkout", "-q", "-b", "feat/scope"})
	mustWrite(t, filepath.Join(dir, "scope.go"), "package x\n")
	mustGit(t, dir, []string{"add", "-A"})
	mustGit(t, dir, []string{"commit", "-q", "-m", "scope"})

	got, err := changedFiles(dir, base)
	if err != nil {
		t.Fatalf("changedFiles: %v", err)
	}
	if !containsString(got, "scope.go") {
		t.Fatalf("changedFiles = %v, want it to contain scope.go", got)
	}

	// A branch with no commits over base yields an empty list.
	mustGit(t, dir, []string{"checkout", "-q", "-b", "feat/nochange", base})
	got, err = changedFiles(dir, base)
	if err != nil {
		t.Fatalf("changedFiles on no-change branch: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("changedFiles on no-change branch = %v, want empty", got)
	}
}

func TestRunGateScoped_FilesPlaceholder(t *testing.T) {
	dir := newProject(t)
	base, _ := pact.At(dir).BaseBranch()
	if base == "" {
		t.Fatal("base branch is empty")
	}

	// Commit the pact scaffolding (AGENTS.md/CLAUDE.md) on the BASE branch so the
	// feature diff below contains only the file this test changes.
	mustGit(t, dir, []string{"add", "-A"})
	mustGit(t, dir, []string{"commit", "-q", "-m", "scaffold"})

	// Create a change so there are changed files to inject.
	mustGit(t, dir, []string{"checkout", "-q", "-b", "feat/files"})
	mustWrite(t, filepath.Join(dir, "changed.go"), "package x\n")
	mustGit(t, dir, []string{"add", "-A"})
	mustGit(t, dir, []string{"commit", "-q", "-m", "change"})

	fe := &fakeExec{exitCode: 0, output: "ok"}
	ok, detail := runGateScoped(context.Background(), fe, dir, "echo {files}", base)
	if !ok {
		t.Fatalf("runGateScoped: want ok=true, got false (detail=%q)", detail)
	}
	if fe.calls != 1 {
		t.Fatalf("runGateScoped calls = %d, want 1", fe.calls)
	}
	if fe.gotCommand != "echo 'changed.go'" {
		t.Errorf("runGateScoped command = %q, want %q", fe.gotCommand, "echo 'changed.go'")
	}
	if got := fe.gotEnv["PACT_CHANGED_FILES"]; got != "changed.go" {
		t.Errorf("PACT_CHANGED_FILES = %q, want %q", got, "changed.go")
	}
}

func TestRunGateScoped_FilesPlaceholderEmptyChangeSetSkips(t *testing.T) {
	dir := newProject(t)
	base, _ := pact.At(dir).BaseBranch()
	if base == "" {
		t.Fatal("base branch is empty")
	}
	mustGit(t, dir, []string{"checkout", "-q", "-b", "feat/empty", base})

	fe := &fakeExec{exitCode: 0, output: "ok"}
	ok, detail := runGateScoped(context.Background(), fe, dir, "echo {files}", base)
	if !ok {
		t.Fatalf("runGateScoped: want ok=true skip-pass, got false (detail=%q)", detail)
	}
	if fe.calls != 0 {
		t.Fatalf("empty change set should skip gate without exec, calls = %d", fe.calls)
	}
	if !strings.Contains(detail, "no changed files") {
		t.Errorf("detail = %q, want 'no changed files'", detail)
	}
}

func TestRunGateScoped_FilesPlaceholderFailClosedOnBadBase(t *testing.T) {
	dir := newProject(t)
	fe := &fakeExec{exitCode: 0, output: "ok"}
	ok, detail := runGateScoped(context.Background(), fe, dir, "echo {files}", "nonexistent-base")
	if ok {
		t.Fatalf("runGateScoped: want ok=false on bad base, got true")
	}
	if fe.calls != 0 {
		t.Fatalf("bad base should fail closed without exec, calls = %d", fe.calls)
	}
	if !strings.Contains(detail, "cannot resolve changed files") {
		t.Errorf("detail = %q, want 'cannot resolve changed files'", detail)
	}
}

func TestRunGateScoped_NonPlaceholderGetsEnv(t *testing.T) {
	dir := newProject(t)
	base, _ := pact.At(dir).BaseBranch()
	if base == "" {
		t.Fatal("base branch is empty")
	}

	mustGit(t, dir, []string{"add", "-A"})
	mustGit(t, dir, []string{"commit", "-q", "-m", "scaffold"})
	mustGit(t, dir, []string{"checkout", "-q", "-b", "feat/env"})
	mustWrite(t, filepath.Join(dir, "env.go"), "package x\n")
	mustGit(t, dir, []string{"add", "-A"})
	mustGit(t, dir, []string{"commit", "-q", "-m", "env"})

	fe := &fakeExec{exitCode: 0, output: "ok"}
	ok, _ := runGateScoped(context.Background(), fe, dir, "echo ok", base)
	if !ok {
		t.Fatalf("runGateScoped: want ok=true, got false")
	}
	if fe.gotCommand != "echo ok" {
		t.Errorf("command = %q, want unchanged 'echo ok'", fe.gotCommand)
	}
	if got := fe.gotEnv["PACT_CHANGED_FILES"]; got != "env.go" {
		t.Errorf("PACT_CHANGED_FILES = %q, want %q", got, "env.go")
	}
}

func mustGit(t *testing.T, dir string, args []string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
