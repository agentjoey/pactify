package cockpit

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func agyPolicyDecision(t *testing.T, command string) (string, string) {
	t.Helper()
	input, err := json.Marshal(map[string]any{"toolCall": map[string]any{"name": "run_command", "args": map[string]string{"CommandLine": command}}})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	RunAgyHook(context.Background(), bytes.NewReader(input), &output, "1")
	var result struct {
		Decision  string `json:"decision"`
		Overwrite struct {
			Command string `json:"CommandLine"`
		} `json:"overwrite"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Decision, result.Overwrite.Command
}

func TestAgyArgumentAllowlistAttacks(t *testing.T) {
	commands := []string{
		`rg --hostname-bin=./hb.sh --hyperlink-format='file://{host}{path}' -H --colors=path:fg:red needle f.txt`,
		"rg --hostname-bin=/bin/sh x", "rg --hostname-bin /bin/sh x", "rg --hyperlink-format=file x", "rg --pre=sh x", "rg --pre-glob=*.go x",
		"rg -z x", "rg --search-zip x", "rg -nz x", "rg --stats x", "rg --unknown x", "rg --glob", "rg -g",
		"tree internal", "tree -o x", "tree --output x", "tree -ox", "tree --output=x",
		"go list std", "go vet ./...", "go list -mod=mod all", "go list -overlay=x ./...", "go list -modfile=x ./...", "go list -pgo=x ./...",
		"go list -gcflags=x ./...", "go list -ldflags=x ./...", "go list -asmflags=x ./...", "go list -m -u all", "go list -export ./...", "go list -compiled ./...",
		"go env -json", "go env -w GOOS=linux", "go env --w GOOS=linux", "go env -u GOOS", "go env GOOS=linux", "go env -- GOOS",
		"git -C /outside log", "git -C/outside log", "git --git-dir=/outside status", "git --git-dir /outside status", "git --work-tree=/outside status", "git --work-tree /outside status", "git --bare log", "git --namespace=x log",
		"git log --git-dir=/outside/.git", "git show --work-tree /outside HEAD", "git diff --no-index /outside/a /outside/b", "git diff --no-ind /outside/a /outside/b", "git diff --no-index=true a b",
		"git log --o=x", "git log --outp x", "git grep --open x", "git show --ext", "git cat-file --filt HEAD:x",
		"grep --pre=x x .", "grep --binary-files=text x .", "egrep --unknown x .", "fgrep --unknown x .", "grep -rz x .", "grep -A", "grep --color=always x .", "grep --color x .", "grep --include",
		"find . -execdir echo x +", "find . -ok echo x +", "find . -delete", "find . -fprint x", "find . -fls x", "find . -printf x", "find . -quit", "find . -unknown", "find . -name",
		"file -C", "file -m magic", "file -mmagic", "file -iC x", "file --compile", "file --magic-file=magic",
		"wc --files0-from=names", "wc --files0-from names", "wc --files0-f=names",
		"du --files0-from=names > output", "stat --printf=%s README.md > output", "cat /dev/* > output", "head /dev/* > output", "tail -f log > output",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			if decision, _ := agyPolicyDecision(t, command); decision != "deny" {
				t.Fatalf("allowed risky command: %q", command)
			}
		})
	}
}

func TestAgyArgumentAllowlistReads(t *testing.T) {
	commands := []string{
		"git log -n 1", "git log --oneline -n 5", "git ls-files internal/orchestrate", `git grep -l "codex-cli"`,
		"ls cmd/pactify", "ls -la internal/sub", "cat .agent/CURRENT.md", `grep -rn "patrol" internal/`, `grep "package sub" internal/sub/*.go`, "find internal/orchestrate -type f", "go doc",
		"grep -RniLl c .", "grep -cwxEFvHhosq -e x .", "grep -A3 -B 2 -C1 x .", "grep --include=*.go --exclude=x --exclude-dir=.git --color=never x .",
		"egrep -rn -e x internal", "fgrep -li x internal", "grep -e --pre file", "grep -- -literal file",
		"rg -nilcwF -e x -g '*.go' -t go -T txt --hidden --no-ignore -A3 -B 2 -C1 -S -s -u .", "rg --files --json --glob=*.go --type=go", "rg -- -literal .",
		"find internal . -name '*.go' -iname '*.GO' -path './internal/*' -ipath './INTERNAL/*' -type f -maxdepth 3 -mindepth 1 -size +1c -mtime -2 -newer README.md -print -print0 -prune -not -name x ! -name y -o -name z -a -type f",
		"find . '(' -name '*.go' -o -name '*.txt' ')' -print", "go env", "go env GOOS GOARCH GOCACHE", "go version", "go doc -u fmt",
		"file -i README.md", "wc -l README.md", "head -n 3 README.md", "tail -n 3 README.md", "stat README.md", "du -sh .", "pwd",
		"du --files0-from=names", "stat --printf=%s README.md", "cat /dev/*", "head /dev/*", "tail -f log",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			decision, rewritten := agyPolicyDecision(t, command)
			if decision != "auto_approve" || rewritten == "" {
				t.Fatalf("read %q: decision=%s rewrite=%q", command, decision, rewritten)
			}
		})
	}
}

func TestAgyQuotedExecutionKeepsArgumentsLiteral(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "expanded.go"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	// Capture the actual argv received after the hook's command crosses a shell.
	if err := os.WriteFile(filepath.Join(bin, "ls"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	decision, rewritten := agyPolicyDecision(t, `ls * ? [ab] {one,two} ~ '(' ')' 'a b' "it's"`)
	if decision != "auto_approve" || rewritten == "" {
		t.Fatalf("decision=%s rewrite=%q", decision, rewritten)
	}
	cmd := exec.Command("sh", "-c", rewritten)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"*", "?", "[ab]", "{one,two}", "~", "(", ")", "a b", "it's"}
	if got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"); !slices.Equal(got, want) {
		t.Fatalf("actual argv=%q want=%q", got, want)
	}
}

func TestAgySettingsExcludeRemovedPrograms(t *testing.T) {
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
	if slices.Contains(settings.Permissions.Allow, "command(tree)") {
		t.Fatalf("tree remains allowed: %s", data)
	}
	if len(settings.Permissions.Allow) != len(agyReadPrograms) {
		t.Fatalf("settings and hook diverged: %s", data)
	}
}
