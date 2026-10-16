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
  -p) echo '{"status":"SUCCESS","num_turns":0,"command":{"name":"usage","data":{"groups":[{"name":"Gemini Models","buckets":[{"id":"gemini-weekly","window":"weekly","remaining_fraction":0.9,"reset_time":"2026-10-23T00:40:01Z"}]}]}}}'; exit 0 ;;
  models)
    [ "$AJ681_LIST_FAIL" = 1 ] && exit 9
    printf 'gemini-3.8-flash-low\tChosen model\n'
    exit 0 ;;
esac
IFS= read -r brief
case " $* " in
  *' --model gemini-3.8-flash-invalid '*)
    echo '{"event":"result","result":{"status":"ERROR","error":"vendor refused invalid model"}}'
    exit 1 ;;
esac
echo '{"event":"init","conversation_id":"probe","init":{"model":"gemini-3.8-flash-low"}}'
case " $* " in
 *' --mode plan '*) echo '{"event":"step_update","step_update":{"step_index":1,"step_type":"tool","state":"ERROR","tool_name":"run_command","tool_info":{"parameters":{"CommandLine":"echo x > probe.txt"},"error":{"message":"permission denied"}}}}' ;;
 *) if [ "$PACTIFY_AGY_READ_ONLY" = 1 ]; then
       echo '{"event":"step_update","step_update":{"step_index":1,"step_type":"tool","state":"ERROR","tool_name":"write_to_file","tool_info":{"parameters":{"TargetFile":"hook-probe.txt"},"error":{"message":"tool call denied by pre-tool hook: Pactify read-only policy"}}}}'
    else echo doctor-ok > out.txt; echo '{"bind":true,"external":true}' > doctor-network.json; fi ;;
esac
mkdir -p "$HOME/.gemini/antigravity-cli"
echo fixture > "$HOME/.gemini/antigravity-cli/conversation_summaries.db"
echo '{"event":"step_update","step_update":{"step_index":2,"step_type":"agent_response","state":"DONE","text_delta":"done","usage":{"input_tokens":1,"output_tokens":1}}}'
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
		{"valid", "gemini-3.8-flash-low", "0", 0, "requested=gemini-3.8-flash-low, actual=gemini-3.8-flash-low"},
		{"invalid", "gemini-3.8-flash-invalid", "0", 1, `unknown agy model "gemini-3.8-flash-invalid"; available: gemini-3.8-flash-low`},
		{"listing-unavailable", "gemini-3.8-flash-low", "1", 0, "requested=gemini-3.8-flash-low, actual=gemini-3.8-flash-low"},
		{"vendor-refusal", "gemini-3.8-flash-invalid", "1", 1, "vendor refused invalid model"},
		{"non-flash-rejected", "argon-low", "0", 1, "requires an available gemini-3.8-flash-* model"},
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
