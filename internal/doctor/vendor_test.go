package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkExec creates a fake executable named command inside dir (a stand-in for a
// vendor CLI on a fake PATH).
func mkExec(t *testing.T, dir, command string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, command), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// mkFile writes content to home/rel, creating parent dirs.
func mkFile(t *testing.T, home, rel, content string) {
	t.Helper()
	p := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findCheck(checks []Check, name string) (Check, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func TestVendorChecks_Binary(t *testing.T) {
	cases := []struct {
		kind    string
		command string
	}{
		{"claude-code", "claude"},
		{"codex-cli", "codex"},
		{"gemini-cli", "gemini"},
		{"kimi-cli", "kimi"},
		{"opencode", "opencode"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			pathDir := t.TempDir()
			home := t.TempDir()

			// absent first: binary not on PATH.
			checks := VendorChecks(home, pathDir)
			c, ok := findCheck(checks, "cli "+tc.kind+": binary")
			if !ok {
				t.Fatalf("missing binary check for %s", tc.kind)
			}
			if c.OK {
				t.Fatalf("%s binary should be absent: %+v", tc.kind, c)
			}
			if !strings.Contains(c.Detail, tc.command) {
				t.Fatalf("absent hint should name the command %q: %+v", tc.command, c)
			}

			// present: drop a fake executable on the fake PATH.
			mkExec(t, pathDir, tc.command)
			checks = VendorChecks(home, pathDir)
			c, _ = findCheck(checks, "cli "+tc.kind+": binary")
			if !c.OK {
				t.Fatalf("%s binary should be found once on PATH: %+v", tc.kind, c)
			}
			if c.Detail != filepath.Join(pathDir, tc.command) {
				t.Fatalf("present detail should be resolved path: %+v", c)
			}
		})
	}
}

// lookInPath must respect the executable bit and ignore directories/non-exec files.
func TestLookInPath_ExecBit(t *testing.T) {
	dir := t.TempDir()
	// non-executable file
	if err := os.WriteFile(filepath.Join(dir, "noexec"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := lookInPath("noexec", dir); ok {
		t.Fatal("non-executable file must not resolve")
	}
	if _, ok := lookInPath("missing", dir); ok {
		t.Fatal("missing command must not resolve")
	}
	mkExec(t, dir, "yes")
	if _, ok := lookInPath("yes", dir); !ok {
		t.Fatal("executable file must resolve")
	}
}

func TestVendorChecks_Auth(t *testing.T) {
	cases := []struct {
		kind    string
		rel     string // credential path relative to HOME; "" = directory case handled specially
		isDir   bool
		lenient bool // absent => still OK (green)
		hasAuth bool // does this kind emit an auth check at all
	}{
		{kind: "claude-code", rel: ".claude/.credentials.json", lenient: true, hasAuth: true},
		{kind: "codex-cli", rel: ".codex/auth.json", lenient: false, hasAuth: true},
		{kind: "gemini-cli", rel: ".gemini/oauth_creds.json", lenient: false, hasAuth: true},
		{kind: "kimi-cli", rel: ".kimi", isDir: true, lenient: true, hasAuth: true},
		{kind: "opencode", hasAuth: false},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			pathDir := t.TempDir()

			// --- absent ---
			absentHome := t.TempDir()
			checks := VendorChecks(absentHome, pathDir)
			c, ok := findCheck(checks, "cli "+tc.kind+": auth")
			if !tc.hasAuth {
				if ok {
					t.Fatalf("%s should not emit an auth check", tc.kind)
				}
				return
			}
			if !ok {
				t.Fatalf("%s should emit an auth check", tc.kind)
			}
			if tc.lenient && !c.OK {
				t.Fatalf("%s absent auth is lenient (should stay green): %+v", tc.kind, c)
			}
			if !tc.lenient && c.OK {
				t.Fatalf("%s absent auth should fail red: %+v", tc.kind, c)
			}

			// --- present ---
			presentHome := t.TempDir()
			if tc.isDir {
				if err := os.MkdirAll(filepath.Join(presentHome, tc.rel), 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				mkFile(t, presentHome, tc.rel, `{"token":"x"}`)
			}
			checks = VendorChecks(presentHome, pathDir)
			c, _ = findCheck(checks, "cli "+tc.kind+": auth")
			if !c.OK {
				t.Fatalf("%s present auth should be green: %+v", tc.kind, c)
			}
			if !strings.Contains(c.Detail, "present") {
				t.Fatalf("%s present detail should note credentials present: %+v", tc.kind, c)
			}
		})
	}
}

// An empty (zero-byte) credential file counts as absent for strict kinds.
func TestVendorChecks_EmptyCredFileIsAbsent(t *testing.T) {
	pathDir := t.TempDir()
	home := t.TempDir()
	mkFile(t, home, ".codex/auth.json", "") // empty
	checks := VendorChecks(home, pathDir)
	c, _ := findCheck(checks, "cli codex-cli: auth")
	if c.OK {
		t.Fatalf("empty codex auth.json should be treated as absent: %+v", c)
	}
}

func TestVendorChecks_TransportClassification(t *testing.T) {
	pathDir := t.TempDir()
	home := t.TempDir()
	checks := VendorChecks(home, pathDir)

	want := map[string]string{
		"kimi-cli":    "acp available",
		"claude-code": "acp available",
		"codex-cli":   "acp available",
		"gemini-cli":  "acp available",
		"opencode":    "cmd only",
	}
	for kind, sub := range want {
		c, ok := findCheck(checks, "cli "+kind+": transport")
		if !ok {
			t.Fatalf("missing transport check for %s", kind)
		}
		if !c.OK {
			t.Fatalf("transport check should always be informational-OK: %+v", c)
		}
		if !strings.Contains(c.Detail, sub) {
			t.Fatalf("%s transport should be %q: %+v", kind, sub, c)
		}
	}
}

// Desktop / non-headless kinds must not appear in the vendor checks at all.
func TestVendorChecks_SkipsNonHeadlessKinds(t *testing.T) {
	checks := VendorChecks(t.TempDir(), t.TempDir())
	for _, c := range checks {
		for _, bad := range []string{"claude-desktop", "antigravity", "codex-app", "cursor-cli"} {
			if strings.Contains(c.Name, bad) {
				t.Fatalf("non-headless kind %s should not produce a check: %+v", bad, c)
			}
		}
	}
}
