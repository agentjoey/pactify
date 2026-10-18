package cockpit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAgyArgumentPolicy(t *testing.T) {
	allowed := []string{
		"git --no-pager log -n 1", "git log -n 1", "git log --oneline -n 5", "git ls-files internal/sub", `git grep -l "codex-cli"`,
		"ls cmd/pactify", "ls -la internal/sub", "cat .agent/CURRENT.md", `grep -rn "patrol" internal/`,
		`grep "package sub" internal/sub/*.go`, "find internal/sub -type f", "go doc",
		"head -n 3 README.md", "tail -n 5 README.md", "wc -l README.md", "stat README.md", "file README.md", "pwd", "du -sh internal",
		"egrep foo README.md", "fgrep foo README.md", "rg --files internal", "find . -name '*.go'", "git branch", "git branch --list -a -r -v",
		"git status --short", "git show HEAD", "git diff --stat", "git ls-tree HEAD", "git blame README.md", "git rev-parse HEAD", "git describe", "git shortlog", "git cat-file -p HEAD", "git for-each-ref",
		"go doc -u fmt", "go env GOOS", "go version", `cat 'a b.txt'`,
	}
	denied := []string{
		"git -C internal log -n 1", "git --git-dir=.git status", "tree internal", "go list ./...", "go vet ./...",
		"git log --out=/x", "git cat-file --filters HEAD:README.md", "git log --output=/x", "git diff -O order", "git grep --open-files-in-pager=sh x", "git grep -Osh x", "git show --ext-diff", "git log --textconv",
		"git -C . -c core.pager=sh log", "git --config-env=core.pager=PAGER log", "git -c core.pager=sh log", "git -ccore.pager=sh log", "git --exec-path=/x log", "git branch new", "git branch -d main", "git branch --list new", "git checkout main",
		"find . -delete", "find . -exec rm {} +", "find . -execdir rm {} +", "find . -ok rm {} +", "find . -okdir rm {} +", "find . -fprint x", "find . -fprint0 x", "find . -fprintf x foo", "find . -fls x",
		"rg --pre sh x", "rg --pre=sh x", "grep --pre-glob=foo x", "egrep --pre sh x", "fgrep --pre sh x",
		"go vet --fix ./...", "go env --w GOOS=linux", "go list --toolexec=sh ./...", "go vet -fix ./...", "go env -w GOOS=linux", "go env -u GOOS", "go list -toolexec=sh ./...", "go vet -vettool=./evil ./...", "go test ./...",
		"sed -n p README.md", "awk '{print}' README.md", "python -c pass", "rm .env", "cat README.md > x.txt", "grep x > y", "ls; pwd", "ls | cat", "ls &", "cat < README.md", "ls $(pwd)", "ls `pwd`", "ls\npwd", "cat <<EOF", "cat 'unclosed", "ls \\", "sh -c ls", "env ls", "ls $ARGS",
	}
	for _, group := range []struct {
		commands []string
		allow    bool
	}{{allowed, true}, {denied, false}} {
		for _, command := range group.commands {
			t.Run(command, func(t *testing.T) {
				input, _ := json.Marshal(map[string]any{"toolCall": map[string]any{"name": "run_command", "args": map[string]string{"CommandLine": command}}})
				var out bytes.Buffer
				RunAgyHook(context.Background(), bytes.NewReader(input), &out, "1")
				var result struct {
					Decision string `json:"decision"`
				}
				if err := json.Unmarshal(out.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if (result.Decision == "auto_approve") != group.allow {
					t.Fatalf("command %q: %s", command, out.String())
				}
			})
		}
	}
}

const flashMalformedCall = "Your previous response contained an improperly formatted function call: Malformed function call: Failed to parse function call: Function call is empty - no input to parse.\nPlease retry with a properly formatted function call\nRetries remaining: 3"

func TestFlashWirePeer(t *testing.T) {
	if os.Getenv("FLASH_WIRE_PEER") != "1" {
		return
	}
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(map[string]any{"event": "init", "conversation_id": "flash-replay", "init": map[string]string{"model": AgyDefaultModel}})
	scan := bufio.NewScanner(os.Stdin)
	for round := 1; scan.Scan(); round++ {
		_ = enc.Encode(map[string]any{"event": "step_update", "step_update": map[string]any{"step_index": 1, "step_type": "agent_response", "state": "ACTIVE", "text_delta": flashMalformedCall}})
		status, failure := "SUCCESS", ""
		if os.Getenv("FLASH_WIRE_TERMINAL") == "1" && round == 1 {
			status, failure = "ERROR", flashMalformedCall
		}
		_ = enc.Encode(map[string]any{"event": "result", "result": map[string]any{"status": status, "error": failure, "response": fmt.Sprintf("round %d completed", round)}})
	}
	os.Exit(0)
}

func TestAgyMalformedWireReplay(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprint(terminal), func(t *testing.T) {
			exe, _ := os.Executable()
			b := &agyBackend{command: func(ctx context.Context, args ...string) *exec.Cmd {
				return exec.CommandContext(ctx, exe, append([]string{"-test.run=^TestFlashWirePeer$", "--"}, args...)...)
			}, grace: time.Second}
			env := []string{"HOME=" + t.TempDir(), "FLASH_WIRE_PEER=1"}
			if terminal {
				env = append(env, "FLASH_WIRE_TERMINAL=1")
			}
			s, err := b.Start(context.Background(), StartOpts{RepoDir: t.TempDir(), Env: env, Access: AccessWrite})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			events := agyTurn(t, s)
			last := events[len(events)-1]
			want := StateTurnCompleted
			if terminal {
				want = StateTurnFailed
			}
			if last.State != want || !bytes.Contains(last.Raw, []byte(`"event":"result"`)) {
				t.Fatalf("terminal=%+v", last)
			}
			if terminal {
				if f := ClassifyFailure("agy", last.Err, last.Raw, time.Now()); f.Class != "transient" {
					t.Fatal(f)
				}
				next := agyTurn(t, s)
				if next[len(next)-1].State != StateTurnCompleted || s.ThreadID() != "flash-replay" {
					t.Fatal(next)
				}
			}
		})
	}
}

func TestAgySettingsProgramPrefixes(t *testing.T) {
	home := t.TempDir()
	if _, err := AgyEnv(StartOpts{MinimalConfig: true, Access: AccessReadOnly, Env: []string{"HOME=" + home}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".gemini/antigravity-cli/settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	for _, program := range []string{"ls", "cat", "head", "tail", "wc", "stat", "file", "pwd", "du", "grep", "egrep", "fgrep", "rg", "find", "git", "go"} {
		if !slices.Contains(settings.Permissions.Allow, "command("+program+")") {
			t.Errorf("missing %s prefix: %s", program, strings.TrimSpace(string(data)))
		}
	}
}
