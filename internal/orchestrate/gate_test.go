package orchestrate

import (
	"context"
	"errors"
	"strings"
	"testing"
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
	exitCode   int
	output     string
	err        error
}

func (f *fakeExec) Run(ctx context.Context, dir, command string) (int, string, error) {
	f.gotDir = dir
	f.gotCommand = command
	return f.exitCode, f.output, f.err
}

func TestGate_RunGate_Pass(t *testing.T) {
	fe := &fakeExec{exitCode: 0, output: "ok\nPASS\n"}
	ok, detail := runGate(context.Background(), fe, "/repo", "go test ./...")
	if !ok {
		t.Fatalf("runGate: want ok=true, got false (detail=%q)", detail)
	}
	if fe.gotDir != "/repo" || fe.gotCommand != "go test ./..." {
		t.Fatalf("runGate: exec got dir=%q command=%q", fe.gotDir, fe.gotCommand)
	}
}

func TestGate_RunGate_NonZeroFails(t *testing.T) {
	fe := &fakeExec{exitCode: 1, output: "--- FAIL: TestRelay\nboom\n"}
	ok, detail := runGate(context.Background(), fe, "/repo", "go test ./...")
	if ok {
		t.Fatalf("runGate: want ok=false on non-zero exit, got true")
	}
	if !strings.Contains(detail, "boom") {
		t.Fatalf("runGate: detail should include output summary, got %q", detail)
	}
}

func TestGate_RunGate_ExecErrorFails(t *testing.T) {
	fe := &fakeExec{exitCode: -1, output: "", err: errors.New("exec: command not found")}
	ok, detail := runGate(context.Background(), fe, "/repo", "nope")
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
