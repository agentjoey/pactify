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
	"github.com/agentjoey/pactify/internal/projection"
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

func TestParseTier(t *testing.T) {
	cases := map[string]Tier{
		"L2":      TierL2,
		"l2":      TierL2,
		" L2 ":    TierL2,
		"L0":      TierL0,
		"L1":      TierL1,
		"L3":      TierL3,
		"":        TierL1,
		"L9":      TierL1,
		"garbage": TierL1,
	}
	for raw, want := range cases {
		if got := ParseTier(raw); got != want {
			t.Errorf("ParseTier(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestGate_ExtractTier(t *testing.T) {
	md := "# Task t1\n\nverify: go test ./...\ntier: L3\n"
	if got := extractTier(md); got != TierL3 {
		t.Fatalf("extractTier: got %q, want %q", got, TierL3)
	}
	// Bare `tier:` with no value is treated as absent.
	if got := extractTier("# spec\ntier:\n"); got != TierL1 {
		t.Fatalf("extractTier(bare) = %q, want %q", got, TierL1)
	}
}

// TestSpecTier pins the three-state contract the plan-review UI depends on:
// present distinguishes "spec has an explicit tier line" from every flavor of
// missing (no line / bare `tier:` / unreadable file / empty specRel) — all of
// which the ENGINE collapses to L1, but which must stay distinguishable from
// an explicit `tier: L1` so a planner-missed tier is visible.
func TestSpecTier(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tasks/l2.md", "# spec\ntier: L2\n")
	write("tasks/none.md", "# spec\nverify: go test ./...\n")
	write("tasks/bare.md", "# spec\ntier:\n")
	write("tasks/lower.md", "# spec\ntier: l3\n")

	cases := []struct {
		name    string
		specRel string
		raw     string
		present bool
	}{
		{"explicit tier", "tasks/l2.md", "L2", true},
		{"lowercase tier is reported raw", "tasks/lower.md", "l3", true},
		{"no tier line", "tasks/none.md", "", false},
		{"bare tier line is absent", "tasks/bare.md", "", false},
		{"missing spec file", "tasks/nope.md", "", false},
		{"empty specRel", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, present := SpecTier(dir, c.specRel)
			if raw != c.raw || present != c.present {
				t.Fatalf("SpecTier(%q) = (%q, %v), want (%q, %v)", c.specRel, raw, present, c.raw, c.present)
			}
		})
	}
}

// TestSpecTier_PathHardening pins the refusal of absolute paths and `..`
// escapes: t.Spec is LLM-generated and lands in an HTTP handler's read path,
// so an out-of-repo specRel must yield ("", false) WITHOUT reading the file —
// proven here by a real out-of-repo file whose tier line must never surface.
func TestSpecTier_PathHardening(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside.md")
	const sentinel = "tier: L3-ESCAPE-SENTINEL"
	if err := os.WriteFile(outside, []byte("# escape\n"+sentinel+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, specRel := range []string{outside, "../outside.md", "..", "a/../../outside.md"} {
		raw, present := SpecTier(dir, specRel)
		if present || raw != "" {
			t.Fatalf("SpecTier(%q) = (%q, %v), want (\"\", false)", specRel, raw, present)
		}
		if strings.Contains(raw, sentinel) {
			t.Fatalf("SpecTier(%q) read the escaped file", specRel)
		}
	}
}

// EffortForTier pins the §4.5 ladder — including the deliberate restraint that
// L2 stays at medium (tier sets the STARTING budget; only real failure buys
// more reasoning).
func TestEffortForTier(t *testing.T) {
	cases := map[Tier]string{
		TierL0: "low",
		TierL1: "medium",
		TierL2: "medium", // deliberate: NOT high
		TierL3: "high",
	}
	for tier, want := range cases {
		if got := EffortForTier(tier); got != want {
			t.Errorf("EffortForTier(%q) = %q, want %q", tier, got, want)
		}
	}
	// Any normalized/unknown tier behaves like the default L1.
	if got := EffortForTier(ParseTier("garbage")); got != "medium" {
		t.Errorf("EffortForTier(unknown) = %q, want medium", got)
	}
}

// launchEffort resolves a stint's budget from the task spec's tier line;
// a spec without one defaults to L1 → medium.
func TestLaunchEffort(t *testing.T) {
	dir := t.TempDir()
	specRel := ".pact/tasks/t1.md"
	if err := os.MkdirAll(filepath.Join(dir, ".pact/tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := Options{Dir: dir}

	mustWrite(t, filepath.Join(dir, specRel), "# t1\nverify: go test ./...\ntier: L0\n")
	if got := opts.launchEffort(projection.Task{Spec: specRel}); got != "low" {
		t.Errorf("launchEffort(L0 spec) = %q, want low", got)
	}

	mustWrite(t, filepath.Join(dir, specRel), "# t1\nverify: go test ./...\ntier: L3\n")
	if got := opts.launchEffort(projection.Task{Spec: specRel}); got != "high" {
		t.Errorf("launchEffort(L3 spec) = %q, want high", got)
	}

	mustWrite(t, filepath.Join(dir, specRel), "# t1\nverify: go test ./...\n")
	if got := opts.launchEffort(projection.Task{Spec: specRel}); got != "medium" {
		t.Errorf("launchEffort(tier-less spec) = %q, want medium", got)
	}

	// An unreadable spec falls back to the L1 default, like the gate's handling.
	if got := opts.launchEffort(projection.Task{Spec: ".pact/tasks/nope.md"}); got != "medium" {
		t.Errorf("launchEffort(missing spec) = %q, want medium", got)
	}
}

// TestGate_ExtractTier_BackwardCompat pins the hard requirement: a spec with
// no `tier:` line sees TierL1 and its verify/QA extraction is byte-for-byte
// unchanged, and a `tier:` line does not disturb `verify:` extraction.
func TestGate_ExtractTier_BackwardCompat(t *testing.T) {
	md := "# Task t1\n\nverify: go test ./internal/serve/\nqa: run the relay e2e\n"

	if got := extractTier(md); got != TierL1 {
		t.Fatalf("extractTier(tier-less spec) = %q, want default %q", got, TierL1)
	}
	cmd, ok := extractVerify(md)
	if !ok || cmd != "go test ./internal/serve/" {
		t.Fatalf("extractVerify changed on tier-less spec: got (%q,%v)", cmd, ok)
	}
	hint, ok := extractQA(md)
	if !ok || hint != "run the relay e2e" {
		t.Fatalf("extractQA changed on tier-less spec: got (%q,%v)", hint, ok)
	}

	// A `tier:` line must not shadow or corrupt the `verify:` directive.
	withTier := md + "tier: L2\n"
	cmd, ok = extractVerify(withTier)
	if !ok || cmd != "go test ./internal/serve/" {
		t.Fatalf("extractVerify with tier line: got (%q,%v)", cmd, ok)
	}
	if got := extractTier(withTier); got != TierL2 {
		t.Fatalf("extractTier = %q, want %q", got, TierL2)
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
