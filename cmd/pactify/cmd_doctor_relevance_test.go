package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/doctor"
	"github.com/agentjoey/pactify/internal/pact"
)

// --- fixtures ---------------------------------------------------------------

// fakeBin drops an executable stub named command into dir.
func fakeBin(t *testing.T, dir, command, body string) {
	t.Helper()
	if body == "" {
		body = "#!/bin/sh\nexit 0\n"
	}
	if err := os.WriteFile(filepath.Join(dir, command), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// doctorSandbox builds a hermetic world in which every NON-vendor doctor check
// passes: a real git repo with an initialized pact ledger, project wiring, a
// fake `pactify` on PATH that answers the MCP handshake, and this test binary's
// own directory on PATH (so the "pactify on PATH" check resolves). It returns
// the directory tests drop fake vendor binaries into.
//
// vendorPath is deliberately separate from the rest so a test can decide, per
// kind, whether that CLI exists.
func doctorSandbox(t *testing.T, seatSpecs ...string) (vendorPath string) {
	t.Helper()
	proj := t.TempDir()
	t.Chdir(proj)
	proj, _ = os.Getwd()

	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t.t"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = proj
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(proj, "base.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", proj, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if out, err := exec.Command("git", "-C", proj, "commit", "-q", "-m", "base").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v %s", err, out)
	}

	seat := strings.SplitN(seatSpecs[0], ":", 2)[0]
	t.Setenv("PACT_AGENT_ID", seat)
	if err := pact.Init("p", seatSpecs); err != nil {
		t.Fatal(err)
	}
	// Project wiring for the "agent wiring" check (and an explicit opencode
	// dependency signal).
	if err := os.WriteFile(filepath.Join(proj, "opencode.json"), []byte(`{"mcp":{"pact":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	return isolateDoctorEnv(t)
}

// isolateDoctorEnv points HOME/PACTIFY_HOME at scratch dirs and rebuilds PATH
// out of (a) a fresh vendor-binary dir and (b) this test binary's own dir, plus
// a `pactify` stub that satisfies the MCP handshake probe. Returns the vendor
// dir.
func isolateDoctorEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PACTIFY_HOME", t.TempDir())

	vendorPath := t.TempDir()
	// checkMCP only asserts the reply mentions "pactify".
	fakeBin(t, vendorPath, "pactify", "#!/bin/sh\necho '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"serverInfo\":{\"name\":\"pactify\"}}}'\n")

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", vendorPath+string(os.PathListSeparator)+filepath.Dir(exe))
	return vendorPath
}

func runDoctor(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newDoctorCmd()
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	return out.String(), err
}

// --- the four required cases ------------------------------------------------

// (1) Regression guard: the project USES opencode and opencode is missing =>
// doctor must still fail.
func TestDoctorExit_UsedKindMissingStillFails(t *testing.T) {
	doctorSandbox(t, "w:worker:AGENTS.md:opencode")
	out, err := runDoctor(t)
	if err == nil {
		t.Fatalf("a missing binary for a kind the project USES must fail doctor\n%s", out)
	}
	if !strings.Contains(out, "✗ cli opencode: binary") {
		t.Fatalf("the used kind must be a hard red on screen:\n%s", out)
	}
}

// (2) The bug: the project does NOT use gemini-cli/codex-cli/antigravity; those
// binaries are missing; doctor must exit ZERO and still list them, marked.
func TestDoctorExit_UnusedKindMissingDoesNotFail(t *testing.T) {
	vendorPath := doctorSandbox(t, "w:worker:AGENTS.md:opencode")
	fakeBin(t, vendorPath, "opencode", "") // the used kind is healthy

	out, err := runDoctor(t)
	if err != nil {
		t.Fatalf("unused vendor CLIs must not fail doctor: %v\n%s", err, out)
	}
	for _, kind := range []string{"gemini-cli", "codex-cli", "antigravity"} {
		line := findLine(t, out, "cli "+kind+": binary")
		if !strings.HasPrefix(line, "!") {
			t.Fatalf("unused failing vendor must carry the advisory mark, got %q", line)
		}
		if !strings.Contains(line, "not used by this project") {
			t.Fatalf("unused failing vendor must say so, got %q", line)
		}
	}
	if !strings.Contains(out, "not used by this project") {
		t.Fatalf("output must explain the advisory marks:\n%s", out)
	}
}

// (3) Outside any pact project: doctor still fails (there is no .pact), but the
// failure is the CORE one — no vendor CLI is ever the reason.
func TestDoctorExit_OutsideAnyPactProject(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	isolateDoctorEnv(t)

	out, err := runDoctor(t, "--json")
	if err == nil {
		t.Fatalf("a dir with no .pact/ must still fail doctor\n%s", out)
	}
	var checks []doctor.Check
	if e := json.Unmarshal([]byte(strings.TrimSpace(out)), &checks); e != nil {
		t.Fatalf("doctor --json is not valid JSON: %v\n%s", e, out)
	}
	sawPactRed, sawVendor := false, false
	for _, c := range checks {
		if strings.HasPrefix(c.Name, "cli ") {
			sawVendor = true
			if !c.OK && !c.Advisory {
				t.Fatalf("outside a project no vendor CLI may gate the exit code: %+v", c)
			}
		}
		if strings.HasPrefix(c.Name, ".pact/") && !c.OK {
			sawPactRed = true
		}
	}
	if !sawVendor {
		t.Fatalf("vendor checks must still be listed outside a project: %s", out)
	}
	if !sawPactRed {
		t.Fatalf("the missing .pact/ must be the reported failure: %s", out)
	}
}

// (4) Mixed: used kind green + unused kind broken => exit zero.
func TestDoctorExit_MixedRelevantOKIrrelevantBroken(t *testing.T) {
	vendorPath := doctorSandbox(t, "w:worker:AGENTS.md:opencode")
	fakeBin(t, vendorPath, "opencode", "")

	out, err := runDoctor(t)
	if err != nil {
		t.Fatalf("relevant kind healthy => doctor must pass: %v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ cli opencode: binary") {
		t.Fatalf("the used kind should be green:\n%s", out)
	}
}

// gating logic is a pure function so the exit-code contract is unit-assertable.
func TestGatingOK(t *testing.T) {
	if !gatingOK([]doctor.Check{{Name: "a", OK: true}, {Name: "b", OK: false, Advisory: true}}) {
		t.Fatal("advisory failures must not gate")
	}
	if gatingOK([]doctor.Check{{Name: "a", OK: true}, {Name: "b", OK: false}}) {
		t.Fatal("a non-advisory failure must gate")
	}
}

func findLine(t *testing.T, out, needle string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}
	t.Fatalf("no line containing %q in:\n%s", needle, out)
	return ""
}
