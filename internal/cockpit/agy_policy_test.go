package cockpit

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgyCompletionTable(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"I started the tests. I will check the results later.", true},
		{"I will report the output once the command finishes.", false},
		{"The tests are still running.", true},
		{"Tests passed: 42 tests, no failures.", false},
		{"The command finished and printed done.", false},
		{"Review the test results in results.txt.", false},
		{"The previous agent said it would check later; I verified all tests passed.", false},
		{"To inspect results later, run cat results.txt.", false},
	} {
		if got := agyNeedsCompletion(tc.text); got != tc.want {
			t.Errorf("%q: got %t", tc.text, got)
		}
	}
}
func TestAgyPolicyReplay(t *testing.T) {
	home := t.TempDir()
	_, err := AgyEnv(StartOpts{MinimalConfig: true, Access: AccessReadOnly, Env: []string{"HOME=" + home}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".gemini", "config", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hooks map[string]json.RawMessage
	if json.Unmarshal(data, &hooks) != nil {
		t.Fatal(string(data))
	}
	for _, tc := range []struct {
		tool, args     string
		readOnly, deny bool
	}{
		{"run_command", `{"CommandLine":"git log --oneline"}`, true, false},
		{"run_command", `{"CommandLine":"git log --oneline > x.txt"}`, true, true},
		{"run_command", `{"CommandLine":"cat README.md > x.txt"}`, true, true},
		{"run_command", `{"CommandLine":"git log | sh"}`, true, true},
		{"run_command", `{"CommandLine":"find . -delete"}`, true, true},
		{"run_command", `{"CommandLine":"find . -exec rm {} +"}`, true, true},
		{"run_command", `{"CommandLine":"git log --output=x.txt"}`, true, true},
		{"run_command", `{"CommandLine":"rm ignored.txt"}`, true, true},
		{"write_to_file", `{"TargetFile":"x.txt"}`, true, true},
		{"view_file", `{"AbsolutePath":"README.md"}`, true, false},
		{"run_command", `{"CommandLine":"echo hi > x.txt","BypassSandbox":true}`, false, true},
		{"run_command", `{"CommandLine":"echo hi > x.txt","BypassSandbox":false}`, false, false},
	} {
		t.Run(tc.tool+tc.args, func(t *testing.T) {
			access := "1"
			if !tc.readOnly {
				access = "0"
			}
			var output bytes.Buffer
			RunAgyHook(context.Background(), strings.NewReader(`{"toolCall":{"name":"`+tc.tool+`","args":`+tc.args+`}}`), &output, access)
			out := output.Bytes()
			var result struct {
				Decision string `json:"decision"`
			}
			_ = json.Unmarshal(out, &result)
			if (result.Decision == "deny") != tc.deny {
				t.Fatalf("out=%s", out)
			}
		})
	}
}

func TestAgyQuotedCompletionPromiseIsNotPending(t *testing.T) {
	if agyNeedsCompletion(`The command printed "I will check the results later." All tests passed.`) {
		t.Fatal("quoted output triggered a completion followup")
	}
}
