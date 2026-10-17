package e2e

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeniedWritesRejectsNullError(t *testing.T) {
	row := []byte(`{"kind":"tool","raw":{"part":{"tool":"bash","state":{"status":"completed","input":{"command":"echo x > denied-shell.txt"},"error":null}}}}`)
	if shell, file := deniedWrites("opencode", [][]byte{row}); shell || file {
		t.Fatal("error:null or a prompt was accepted as an actual denial")
	}
}

func TestDeniedWritesStructuredEvidence(t *testing.T) {
	for _, tc := range []struct {
		harness string
		rows    []string
	}{
		{"opencode", []string{
			`{"kind":"tool","raw":{"part":{"tool":"bash","state":{"status":"error","input":{"command":"echo x > denied-shell.txt"},"error":"Permission denied"}}}}`,
			`{"kind":"tool","raw":{"part":{"tool":"write","state":{"status":"error","input":{"filePath":"/repo/denied-tool.txt"},"error":"The user rejected permission"}}}}`,
		}},
		{"agy", []string{
			`{"kind":"tool","raw":{"step_update":{"step_index":1,"tool_name":"run_command","state":"ACTIVE","tool_info":{"parameters":{"CommandLine":"echo x > denied-shell.txt"}}}}}`,
			`{"kind":"tool","raw":{"step_update":{"step_index":1,"tool_name":"run_command","state":"ERROR","tool_info":{"error":{"message":"Operation not permitted"}}}}}`,
			`{"kind":"tool","raw":{"step_update":{"step_index":2,"tool_name":"write_to_file","state":"ERROR","tool_info":{"parameters":{"TargetFile":"/repo/denied-tool.txt"},"error":{"message":"denied by pre-tool hook"}}}}}`,
		}},
		{"codex", []string{
			`{"kind":"tool","raw":{"item":{"type":"commandExecution","command":"echo x > denied-shell.txt","exitCode":1,"aggregatedOutput":"Read-only file system"}}}`,
			`{"kind":"tool","raw":{"item":{"type":"fileChange","status":"declined","changes":[{"path":"/repo/denied-tool.txt"}]}}}`,
		}},
	} {
		t.Run(tc.harness, func(t *testing.T) {
			rows := [][]byte{}
			for _, row := range tc.rows {
				rows = append(rows, []byte(row))
			}
			if shell, file := deniedWrites(tc.harness, rows); !shell || !file {
				t.Fatalf("structured denials missing: shell=%t file=%t", shell, file)
			}
		})
	}
}

func TestLongToolRequiresActualOutputAndRecordedDuration(t *testing.T) {
	command := []byte(`{"kind":"tool","tool":{"phase":"start","text":"sleep 240 && echo E2E_LONG_DONE"}}`)
	output := []byte(`{"kind":"tool","tool":{"phase":"output","text":"E2E_LONG_DONE\n"}}`)
	for _, tc := range []struct {
		name, duration string
		rows           [][]byte
		want           bool
	}{
		{"real", "4m01s", [][]byte{command, output}, true},
		{"prompt only", "4m01s", [][]byte{command}, false},
		{"short job", "3m59s", [][]byte{command, output}, false},
		{"final reply only", "4m01s", [][]byte{command, []byte(`{"kind":"message","text":"E2E_LONG_DONE","final":true}`)}, false},
		{"no command", "4m01s", [][]byte{output}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := longToolEvidence(tc.rows, tc.duration); (err == nil) != tc.want {
				t.Fatalf("evidence check=%v want success=%t", err, tc.want)
			}
		})
	}
}

func TestLongToolKimiInputStreamingIsNotOutput(t *testing.T) {
	command := []byte(`{"kind":"tool","raw":{"sessionUpdate":"tool_call","rawInput":{"command":"sleep 240 && echo E2E_LONG_DONE"}}}`)
	stream := []byte(`{"kind":"tool","raw":{"sessionUpdate":"tool_call_update","status":"in_progress","content":[{"content":{"type":"text","text":"sleep 240 && echo E2E_LONG_DONE"}}]}}`)
	if err := longToolEvidence([][]byte{command, stream}, "4m01s"); err == nil {
		t.Fatal("streamed tool arguments mistaken for output")
	}
	output := []byte(`{"kind":"tool","raw":{"sessionUpdate":"tool_call_update","status":"completed","content":[{"content":{"type":"text","text":"E2E_LONG_DONE\n"}}]}}`)
	if err := longToolEvidence([][]byte{command, output}, "4m01s"); err != nil {
		t.Fatal(err)
	}
}

// Real kimi 2.2.0 shape (testdata/golden/kimi-write.json): tool_call carries
// only the title; the arguments arrive as in-progress JSON text.
func TestLongToolKimiStreamedArgumentsAreCommandEvidence(t *testing.T) {
	start := []byte(`{"kind":"tool","tool":{"phase":"start","name":"Bash"},"raw":{"sessionUpdate":"tool_call","status":"pending","title":"Bash"}}`)
	args := []byte(`{"kind":"tool","raw":{"sessionUpdate":"tool_call_update","status":"in_progress","content":[{"type":"content","content":{"type":"text","text":"{\"command\":\"sleep 240 && echo E2E_LONG_DONE\""}}]}}`)
	output := []byte(`{"kind":"tool","raw":{"sessionUpdate":"tool_call_update","status":"completed","content":[{"type":"content","content":{"type":"text","text":"E2E_LONG_DONE\n"}}]}}`)
	if err := longToolEvidence([][]byte{start, args, output}, "4m14s"); err != nil {
		t.Fatal(err)
	}
	if err := longToolEvidence([][]byte{start, output}, "4m14s"); err == nil {
		t.Fatal("output without any command evidence accepted")
	}
	if err := longToolEvidence([][]byte{start, args}, "4m14s"); err == nil {
		t.Fatal("streamed arguments accepted as output")
	}
}

func TestHostSecureStorageAndAuthPreflightFake(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			dir := t.TempDir()
			fake := filepath.Join(dir, "claude-fake")
			wantSecure := "/fixture-user/.claude"
			if platform == "darwin" {
				wantSecure = ""
			}
			script := fmt.Sprintf("#!/bin/sh\n[ \"$1 $2 $3\" = 'auth status --json' ] || exit 7\n[ \"$CLAUDE_SECURESTORAGE_CONFIG_DIR\" = '%s' ] || exit 8\nprintf '{\"loggedIn\":true}\\n'\n", wantSecure)
			if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			env := hostEnvironment([]string{"PATH=/usr/bin:/bin", "CLAUDE_CONFIG_DIR=old", "CLAUDE_SECURESTORAGE_CONFIG_DIR=wrong"}, "/fixture-user", dir, platform)
			if err := hostAuthStatus(func(args ...string) ([]byte, error) {
				cmd := exec.Command(fake, args...)
				cmd.Env = env
				return cmd.Output()
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(filepath.Join(dir, ".credentials.json")); !os.IsNotExist(err) {
				t.Fatal("host fixture contains a credential symlink")
			}
		})
	}
	if err := hostAuthStatus(func(args ...string) ([]byte, error) { return []byte(`{"loggedIn":false}`), nil }); err == nil {
		t.Fatal("logged-out host passed")
	}
}

func TestKimiReadonlyRequiresExitTwoAndDiagnosticFake(t *testing.T) {
	for _, tc := range []struct {
		code int
		text string
		want bool
	}{
		{2, "kimi does not offer read-only access: it cannot enforce it", true},
		{0, "kimi cannot enforce read-only", false},
		{1, "kimi cannot enforce read-only", false},
		{2, "unrelated failure", false},
	} {
		cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf("exit %d", tc.code))
		err := kimiReadonlyRejection([]byte(tc.text), cmd.Run())
		if (err == nil) != tc.want {
			t.Errorf("exit=%d text=%s: %v", tc.code, tc.text, err)
		}
	}
}

func TestKimiReadonlyProbeCancelsUnexpectedJobFake(t *testing.T) {
	for _, failCancel := range []bool{false, true} {
		called := ""
		err := kimiReadonlyProbe(func() ([]byte, error) {
			return exec.Command("/bin/sh", "-c", `printf '{"jobId":"unexpected-job"}'`).Output()
		}, func(id string) error {
			called = id
			if failCancel {
				return fmt.Errorf("fake cancel failure")
			}
			return nil
		})
		if called != "unexpected-job" || err == nil {
			t.Fatalf("unexpected job was not canceled and failed: cancel=%q err=%v", called, err)
		}
	}
}

func TestEvidenceIgnoresNonToolRecords(t *testing.T) {
	row, _ := json.Marshal(map[string]any{"kind": "message", "raw": map[string]any{"permission": "denied-shell.txt denied-tool.txt", "error": "denied"}})
	if shell, file := deniedWrites("opencode", [][]byte{row}); shell || file {
		t.Fatal("prose accepted as tool evidence")
	}
}

func TestFixtureRetainsNestedUnpreservedRefresh(t *testing.T) {
	for _, tc := range []struct {
		warning string
		want    bool
	}{
		{"fake private credential refresh retained at /fixture/nested/auth.json; preservation failed: rename error", true},
		{"fake private copy retained at /fixture/nested/credentials: rename error", true},
		{"fake source metadata unavailable: unreadable; private refreshes retained", true},
		{"fake credential refresh conflict quarantined at /quarantine/credential; source was not overwritten", false},
		{"Credential refresh returned to source: /fake/auth.json", false},
	} {
		if got := requiresCredentialRetention([]string{tc.warning}); got != tc.want {
			t.Errorf("retention=%t want %t: %s", got, tc.want, tc.warning)
		}
	}
}

func TestCredentialMetadataRefreshBoundaries(t *testing.T) {
	base := "/user/.kimi-code/"
	old := meta{Inode: 1, Mode: 0600, Size: 10, Mtime: 1}
	for _, tc := range []struct {
		name    string
		a, b    map[string]meta
		refresh bool
	}{
		{"refresh", map[string]meta{base + "credentials/kimi-code.json": old}, map[string]meta{base + "credentials/kimi-code.json": {Inode: 2, Mode: 0600, Size: 20, Mtime: 2}}, true},
		{"temporary", map[string]meta{}, map[string]meta{base + "credentials/kimi-code.json.tmp.123.456": old}, false},
		{"existing temporary", map[string]meta{base + "credentials/kimi-code.json.tmp.123.456": old}, map[string]meta{base + "credentials/kimi-code.json.tmp.123.456": {Mode: 0600, Inode: 2}}, false},
		{"permissions", map[string]meta{base + "credentials/kimi-code.json": old}, map[string]meta{base + "credentials/kimi-code.json": {Inode: 1, Mode: 0644}}, false},
		{"already insecure", map[string]meta{base + "credentials/kimi-code.json": {Mode: 0644, Inode: 1}}, map[string]meta{base + "credentials/kimi-code.json": {Mode: 0644, Inode: 2}}, false},
		{"link replaced", map[string]meta{base + "credentials/kimi-code.json": {Mode: fs.ModeSymlink | 0777, Target: "/elsewhere"}}, map[string]meta{base + "credentials/kimi-code.json": old}, false},
		{"lock created", map[string]meta{}, map[string]meta{base + "oauth/kimi-code": old}, true},
		{"default-mode lock created", map[string]meta{}, map[string]meta{base + "oauth/kimi-code": {Mode: 0644, Inode: 1}}, true},
		{"default-mode lock refreshed", map[string]meta{base + "oauth/kimi-code": {Mode: 0644, Inode: 1}}, map[string]meta{base + "oauth/kimi-code": {Mode: 0644, Inode: 2}}, true},
		{"lock removed", map[string]meta{base + "oauth/kimi-code": old}, map[string]meta{}, true},
		{"lock directory mtime", map[string]meta{base + "oauth": {Inode: 1, Mode: fs.ModeDir | 0700, Mtime: 1}}, map[string]meta{base + "oauth": {Inode: 1, Mode: fs.ModeDir | 0700, Mtime: 2}}, true},
		{"lazy lock directory created", map[string]meta{}, map[string]meta{base + "oauth": {Inode: 1, Mode: fs.ModeDir | 0700, Mtime: 2}}, true},
		{"existing lock directory mode", map[string]meta{base + "oauth": {Inode: 1, Mode: fs.ModeDir | 0755, Mtime: 1}}, map[string]meta{base + "oauth": {Inode: 1, Mode: fs.ModeDir | 0755, Mtime: 2}}, true},
		{"lock permissions", map[string]meta{base + "oauth/kimi-code": old}, map[string]meta{base + "oauth/kimi-code": {Mode: 0644, Inode: 1}}, false},
		{"lock temporary", map[string]meta{}, map[string]meta{base + "oauth/kimi-code.tmp.1.2": old}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed, refresh := compareMetadata(tc.a, tc.b, "kimi")
			if tc.refresh && (len(changed) != 0 || len(refresh) == 0) {
				t.Errorf("legitimate refresh/lock rejected: changed=%v refresh=%v", changed, refresh)
			}
			if !tc.refresh && len(changed) == 0 {
				t.Error("unsafe metadata change exempted")
			}
		})
	}
}

func TestCodexRouterPatchRejectionCountsAsDeniedFile(t *testing.T) {
	line := []byte("\x1b[2m2026-10-17T07:53:20Z\x1b[0m \x1b[31mERROR\x1b[0m \x1b[2mcodex_core::tools::router\x1b[0m: error=patch rejected: writing is blocked by read-only sandbox; rejected by user approval settings")
	if _, file := deniedWrites("codex", [][]byte{line}); !file {
		t.Fatal("codex router patch rejection was not counted as a denied file write")
	}
	other := []byte("\x1b[31mERROR\x1b[0m codex_core::tools::router: error=write_stdin failed: Unknown process id 83339")
	if shell, file := deniedWrites("codex", [][]byte{other}); shell || file {
		t.Fatal("an unrelated router error counted as a denial")
	}
}

func TestHostEnvironmentKeepsUserHomeOnDarwin(t *testing.T) {
	envValue := func(env []string, key string) string {
		for _, v := range env {
			if strings.HasPrefix(v, key+"=") {
				return strings.TrimPrefix(v, key+"=")
			}
		}
		return ""
	}
	env := hostEnvironment([]string{"HOME=/fixture/home", "PATH=/bin"}, "/Users/me", "/fixture/claude", "darwin")
	if envValue(env, "HOME") != "/Users/me" || envValue(env, "CLAUDE_CONFIG_DIR") != "/fixture/claude" {
		t.Fatalf("darwin host env = %v", env)
	}
	env = hostEnvironment([]string{"HOME=/fixture/home"}, "/home/me", "/fixture/claude", "linux")
	if envValue(env, "HOME") != "/fixture/home" {
		t.Fatalf("linux host env changed HOME: %v", env)
	}
}
