package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubDoctorModelCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	// Only the opt-in fixture can run; no installed vendor CLI is reachable.
	t.Setenv("PATH", "/usr/bin:/bin")
	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"explicit", []string{"fake", "--model", "minimax-coding-plan/MiniMax-M3"}, 0, "requested=minimax-coding-plan/MiniMax-M3, actual=minimax-coding-plan/MiniMax-M3"},
		{"flag-first", []string{"--model", "chosen", "fake"}, 0, "requested=chosen, actual=chosen"},
		{"no-harness", []string{"--model", "chosen"}, 2, "--model requires exactly one harness"},
		{"empty-no-harness", []string{"--model="}, 2, "--model requires exactly one harness"},
		{"multiple", []string{"fake", "codex", "--model", "chosen"}, 2, "at most 1"},
		{"unknown", []string{"unknown", "--model", "chosen"}, 2, "unknown harness"},
		{"missing-value", []string{"fake", "--model"}, 2, "flag needs an argument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, code := runCLI(t, bin, t.TempDir(), "", append([]string{"sub", "doctor"}, tc.args...)...)
			if code != tc.code || !strings.Contains(out, tc.want) {
				t.Fatalf("code=%d want=%d output=%s", code, tc.code, out)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(home, "jobs")); !os.IsNotExist(err) {
		t.Fatalf("doctor polluted jobs: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "verified-versions.json"))
	if err != nil || !strings.Contains(string(data), `"fake"`) || strings.Contains(string(data), "chosen") {
		t.Fatalf("verified table=%s err=%v", data, err)
	}
}

func TestSubDoctorModelAgyValidationCLI(t *testing.T) {
	bin := buildPactify(t)
	agyBin := t.TempDir()
	// Exercise the real agy backend with a local CLI fixture. Both model-list
	// errors and model refusals come from that backend, as they do for sub run.
	script := `#!/bin/sh
case "$1" in
  --version) echo 'agy test'; exit 0 ;;
  models)
    [ "$AJ681_LIST_FAIL" = 1 ] && exit 9
    printf 'chosen\tChosen model\n'
    exit 0 ;;
esac
IFS= read -r brief
case " $* " in
  *' --model invalid '*)
    echo '{"event":"result","result":{"status":"ERROR","error":"vendor refused invalid model"}}'
    exit 1 ;;
esac
echo '{"event":"init","conversation_id":"probe","init":{"model":"chosen"}}'
echo doctor-ok > out.txt
mkdir -p "$HOME/.gemini/antigravity-cli"
echo fixture > "$HOME/.gemini/antigravity-cli/conversation_summaries.db"
echo '{"event":"result","result":{"status":"SUCCESS","response":"done","usage":{"input_tokens":1,"output_tokens":1}}}'
`
	if err := os.WriteFile(filepath.Join(agyBin, "agy"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", agyBin+string(os.PathListSeparator)+"/usr/bin:/bin")
	for _, tc := range []struct {
		name     string
		model    string
		listFail string
		code     int
		want     string
	}{
		{"valid", "chosen", "0", 0, "requested=chosen, actual=chosen"},
		{"invalid", "invalid", "0", 1, `unknown agy model "invalid"; available: chosen`},
		{"listing-unavailable", "chosen", "1", 0, "requested=chosen, actual=chosen"},
		{"vendor-refusal", "invalid", "1", 1, "vendor refused invalid model"},
		{"default-needs-listing", "", "1", 1, "cannot list models"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("PACTIFY_HOME", home)
			t.Setenv("HOME", t.TempDir())
			t.Setenv("AJ681_LIST_FAIL", tc.listFail)
			token := filepath.Join(os.Getenv("HOME"), ".gemini", "antigravity-cli", "antigravity-oauth-token")
			if err := os.MkdirAll(filepath.Dir(token), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(token, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"sub", "doctor", "agy"}
			if tc.model != "" {
				args = append(args, "--model", tc.model)
			}
			out, code := runCLI(t, bin, t.TempDir(), "", args...)
			if code != tc.code || !strings.Contains(out, tc.want) {
				t.Fatalf("code=%d want=%d output=%s", code, tc.code, out)
			}
			_, err := os.Stat(filepath.Join(home, "verified-versions.json"))
			if (tc.code == 0 && err != nil) || (tc.code != 0 && !os.IsNotExist(err)) {
				t.Fatalf("verified table on exit %d: %v", tc.code, err)
			}
		})
	}
}
