package sub

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateFinalRespectsUTF8(t *testing.T) {
	for _, tc := range []struct {
		text string
		size int
		cut  bool
	}{{"", 0, false}, {strings.Repeat("a", 4096), 4096, false}, {strings.Repeat("a", 4097), 4096, true}, {strings.Repeat("好", 2000), 4095, true}} {
		got, cut := TruncateFinal(tc.text)
		if len(got) != tc.size || cut != tc.cut || !utf8.ValidString(got) {
			t.Fatalf("len=%d cut=%v valid=%v", len(got), cut, utf8.ValidString(got))
		}
	}
}

func TestMarkdownFlagsModelMismatchFirst(t *testing.T) {
	r := Result{Status: StatusSucceeded, Harness: "kimi", ModelRequested: "kimi-code/kimi-for-coding", ModelActual: "kimi-code/k3-256k", Warnings: []string{"Actual model does not match the requested model", "Tool-call limit exceeded"}, FinalText: "done", LogPath: "/tmp/l"}
	md := r.Markdown()
	first := strings.SplitN(md, "\n", 2)[0]
	if !strings.Contains(first, "Actual model does not match the requested model; Tool-call limit exceeded") {
		t.Fatalf("first=%q", first)
	}
	for _, want := range []string{"kimi", "kimi-code/kimi-for-coding", "kimi-code/k3-256k", "unverified", "done", "/tmp/l"} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q:\n%s", want, md)
		}
	}
}

func TestMarkdownFailedWithoutWarningsIsNotSuccess(t *testing.T) {
	for _, status := range []Status{StatusFailed, StatusCanceled} {
		t.Run(string(status), func(t *testing.T) {
			r := Result{Status: status, Harness: "codex", FinalText: "failure details\nmore details"}
			first := strings.SplitN(r.Markdown(), "\n", 2)[0]
			if want := "✗ codex " + string(status); first != want {
				t.Fatalf("first line = %q, want %q", first, want)
			}
		})
	}
}

func TestMarkdownIncludesMetadataAndLimitsFiles(t *testing.T) {
	r := Result{Status: StatusSucceeded, Harness: "codex", CLIVersion: "0.162.1", ModelRequested: "gpt-test", ModelActual: "gpt-test", ModelVerified: true, Access: "write", Duration: "2s", InputTokens: 100, OutputTokens: 20, CostUSD: 0.01, ToolCalls: 3, FinalText: "result\n```\nembedded fence", SessionID: "th-9", LogPath: "/tmp/log"}
	for i := 0; i < 21; i++ {
		r.Diff.Files = append(r.Diff.Files, FileDelta{Path: fmt.Sprintf("file-%02d", i), Added: 1})
	}
	r.Diff.Added = 21
	md := r.Markdown()
	if !strings.HasPrefix(md, "✓ codex succeeded\n") {
		t.Fatalf("heading=%s", md)
	}
	for _, want := range []string{"0.162.1", "gpt-test", "write", "2s", "100", "20", "0.01", "3", "21", "+21", "file-19", "th-9", "/tmp/log", "result\n```\nembedded fence"} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "file-20") || strings.Contains(md, "unverified") {
		t.Fatalf("unexpected content:\n%s", md)
	}
	if !strings.Contains(md, "````\nresult") {
		t.Fatalf("reply escaped its code fence:\n%s", md)
	}
}

// No --model: the summary must say the harness default was used, not print an empty slot.
func TestMarkdownNamesDefaultModelWhenNoneRequested(t *testing.T) {
	r := Result{Status: StatusSucceeded, Harness: "codex", ModelActual: "gpt-6.1-sol", ModelVerified: true}
	if md := r.Markdown(); !strings.Contains(md, "Model: (default) → gpt-6.1-sol") {
		t.Fatalf("model line:\n%s", md)
	}
}

// Q4: codex's account/rateLimits/updated must reach the summary, not only the job log.
func TestSummarizeRateLimit(t *testing.T) {
	raw := []byte(`{"rateLimits":{"limitId":"codex","primary":{"usedPercent":10,"windowDurationMins":10080,"resetsAt":1792606731},"secondary":null}}`)
	got := SummarizeRateLimit(raw)
	if !strings.Contains(got, "10%") || !strings.Contains(got, "7-day window") {
		t.Fatalf("summary = %q", got)
	}
	if SummarizeRateLimit(nil) != "" || SummarizeRateLimit([]byte(`{"x":1}`)) != "" {
		t.Fatal("no rate limit data must summarize to empty")
	}
	r := Result{Status: StatusSucceeded, Harness: "codex", RateLimit: got}
	if !strings.Contains(r.Markdown(), "Quota: "+got) {
		t.Fatalf("markdown lacks rate limit:\n%s", r.Markdown())
	}
}

func TestMarkdownEnglishLabels(t *testing.T) {
	r := Result{Status: StatusSucceeded, Harness: "codex", CLIVersion: "test", ModelActual: "actual", Access: "write", Duration: "1s", InputTokens: 2, OutputTokens: 3, CostUSD: 0.01, ToolCalls: 4, RateLimit: "quota", WorktreePath: "/tmp/wt", BaseCommit: "abc", FinalText: "reply", Truncated: true, SessionID: "session", LogPath: "/tmp/log"}
	for i := 0; i < 21; i++ {
		r.Diff.Files = append(r.Diff.Files, FileDelta{Path: fmt.Sprintf("file-%d", i)})
	}
	md := r.Markdown()
	for _, want := range []string{
		"✓ codex succeeded\n", "Harness: codex · CLI: test\n",
		"Model: (default) → actual (unverified)\n", "Access: write\n", "Duration: 1s\n",
		"Tokens: in 2 · out 3 · Cost $0.010000\n", "Quota: quota\n", "Tool calls: 4\n",
		"Changes: 21 files · +0/-0\n", "Worktree: /tmp/wt\nBase commit: abc\n",
		"…more files: 1\n", "Final reply:\n```\nreply\n```\n(Final reply truncated)\n",
		"Session: session\nLog: /tmp/log\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}

func TestMarkdownPluralizesChangedFiles(t *testing.T) {
	one := Result{Status: StatusSucceeded, Harness: "codex", Diff: Diff{Files: []FileDelta{{Path: "a", Added: 1}}, Added: 1}}
	if md := one.Markdown(); !strings.Contains(md, "Changes: 1 file · +1/-0") {
		t.Fatalf("singular:\n%s", md)
	}
	two := Result{Status: StatusSucceeded, Harness: "codex", Diff: Diff{Files: []FileDelta{{Path: "a"}, {Path: "b"}}}}
	if md := two.Markdown(); !strings.Contains(md, "Changes: 2 files") {
		t.Fatalf("plural:\n%s", md)
	}
}
