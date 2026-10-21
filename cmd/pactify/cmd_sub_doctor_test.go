package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubDoctorCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	// No real agent can start, including the no-argument path.
	t.Setenv("PATH", "/usr/bin:/bin")
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"fake"}, 0, "PASS"},
		{nil, 0, "SKIP Installed: not installed"},
		{[]string{"codex"}, 1, "not installed"},
		{[]string{"unknown"}, 2, "unknown harness"},
		{[]string{"fake", "codex"}, 2, "at most 1"},
		{[]string{"--unknown"}, 2, "unknown flag"},
	} {
		out, code := runCLI(t, bin, t.TempDir(), "", append([]string{"sub", "doctor"}, tc.args...)...)
		if code != tc.code || !strings.Contains(out, tc.want) {
			t.Errorf("args=%v code=%d output=%s", tc.args, code, out)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "jobs")); !os.IsNotExist(err) {
		t.Fatalf("doctor polluted jobs: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "verified-versions.json"))
	if err != nil || !strings.Contains(string(data), `"fake"`) {
		t.Fatalf("verified table=%s err=%v", data, err)
	}
}
