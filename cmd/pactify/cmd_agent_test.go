package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/agentreg"
)

// noAgentsInstalled makes every agent kind probe as NOT installed, hermetically:
// CLI kinds resolve via exec.LookPath (PATH → an empty dir), desktop/global kinds
// via a "~/..." config path (HOME → an empty dir). PACTIFY_HOME is redirected too
// so the agent registry written by these tests never touches ~/.pactify.
func noAgentsInstalled(t *testing.T) {
	t.Helper()
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
}

// runAgentCmd executes `pactify agent <args...>` in-process and returns stdout,
// stderr and the error. Warnings may land on either stream; tests that only care
// that the user saw something assert against the concatenation.
func runAgentCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := newAgentCmd()
	cmd.SetArgs(args)
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SilenceUsage = true
	err = cmd.Execute()
	return out.String(), errb.String(), err
}

// [REGISTER] `agent register <kind>` used to be a pure static map lookup: a kind
// whose binary is nowhere on this machine registered silently, and the user only
// found out when an orchestrate run failed to launch it. The installed-check lived
// only in the separate `agent scan`.
//
// Registering anyway is DELIBERATE (the dashboard's "Add manually / Register
// anyway" flow registers supported-but-undetected kinds, and users legitimately
// register ahead of installing) — so this must warn, not fail.
func TestAgentRegisterWarnsWhenKindNotInstalled(t *testing.T) {
	noAgentsInstalled(t)

	stdout, stderr, err := runAgentCmd(t, "register", "opencode")
	if err != nil {
		t.Fatalf("register must NOT hard-fail for an uninstalled kind: %v", err)
	}
	got := stdout + stderr

	low := strings.ToLower(got)
	if !strings.Contains(low, "not installed") {
		t.Errorf("register of an uninstalled kind must warn %q is not installed; output was %q", "opencode", got)
	}
	// Actionable: name what is missing (the binary the probe looked for) and how
	// to confirm the fix. A bare "not installed" leaves the user guessing.
	if !strings.Contains(got, "opencode") {
		t.Errorf("warning must name the missing binary/kind; output was %q", got)
	}
	if !strings.Contains(got, "agent scan") {
		t.Errorf("warning must point at `pactify agent scan` to re-check after installing; output was %q", got)
	}

	// ...and it must still register.
	reg, lerr := agentreg.Load()
	if lerr != nil {
		t.Fatal(lerr)
	}
	if !reg.Has("opencode") {
		t.Fatal("register must still register the kind after warning (register-anyway workflow)")
	}
}

// The happy path stays quiet: an installed kind must not emit a scary warning.
func TestAgentRegisterQuietWhenKindInstalled(t *testing.T) {
	noAgentsInstalled(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	stdout, stderr, err := runAgentCmd(t, "register", "opencode")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if got := stdout + stderr; strings.Contains(strings.ToLower(got), "not installed") {
		t.Errorf("installed kind must not warn; output was %q", got)
	}
}

// An unknown kind is still a hard error (unchanged): there is nothing to register.
func TestAgentRegisterUnknownKindStillErrors(t *testing.T) {
	noAgentsInstalled(t)
	if _, _, err := runAgentCmd(t, "register", "not-a-real-kind"); err == nil {
		t.Fatal("unknown kind must remain a hard error")
	}
}
