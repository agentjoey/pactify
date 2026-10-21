package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubAgyInvalidEffortBeforeLaunch(t *testing.T) {
	bin := buildPactify(t)
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "/usr/bin:/bin")
	out, code := runCLI(t, bin, t.TempDir(), "review", "sub", "run", "agy", "--effort", "maximum", "--wait", "0s")
	if code != 2 || !strings.Contains(out, "unsupported effort") {
		t.Fatalf("code=%d out=%s", code, out)
	}
	store := os.Getenv("PACTIFY_HOME") + "/jobs"
	if files, _ := os.ReadDir(store); len(files) != 0 {
		t.Fatal(files)
	}
}

func TestSubAgyExplicitModelEffortPriority(t *testing.T) {
	bin := buildPactify(t)
	t.Setenv("HOME", t.TempDir())
	stubUnavailableHarness(t, "agy") // Inert CLI; no real harness or login is used.
	for _, tc := range []struct {
		name   string
		args   []string
		effort string
	}{
		{"cli-model", []string{"agy", "--model", "gemini-3.8-flash-low"}, ""},
		{"empty-model", []string{"agy", "--model="}, ""},
		{"alias-model", []string{"@tier"}, ""},
		{"cli-effort", []string{"agy", "--model", "gemini-3.8-flash-low", "--effort", "low"}, "low"},
		{"empty-effort", []string{"@effort", "--effort="}, ""},
		{"alias-effort", []string{"@effort", "--model", "gemini-3.8-flash-low"}, "low"},
		{"default", []string{"agy"}, "high"},
		{"alias-without-model", []string{"@no-model"}, "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("PACTIFY_HOME", home)
			config := `{"defaults":{"agy":{"model":"default-model","effort":"high"}},"aliases":{"tier":{"harness":"agy","model":"gemini-3.8-flash-low"},"effort":{"harness":"agy","effort":"low"},"no-model":{"harness":"agy"}}}`
			if err := os.WriteFile(filepath.Join(home, "router.json"), []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"sub", "run"}, tc.args...)
			args = append(args, "--wait", "5s", "--json")
			out, _ := runCLI(t, bin, t.TempDir(), "brief", args...)
			var result sub.Result
			if err := json.Unmarshal([]byte(out), &result); err != nil || result.JobID == "" {
				t.Fatalf("result: %s (%v)", out, err)
			}
			store, _ := sub.DefaultStore()
			st, err := store.Load(result.JobID)
			if err != nil || st.Request.Effort != tc.effort {
				t.Fatalf("request=%+v err=%v; want effort %q", st.Request, err, tc.effort)
			}
		})
	}
}
