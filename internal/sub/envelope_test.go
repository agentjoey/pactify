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
	r := Result{Status: StatusSucceeded, Harness: "kimi", ModelRequested: "kimi-code/kimi-for-coding", ModelActual: "kimi-code/k3-256k", Warnings: []string{"实际模型与请求不一致", "工具调用超限"}, FinalText: "done", LogPath: "/tmp/l"}
	md := r.Markdown()
	first := strings.SplitN(md, "\n", 2)[0]
	if !strings.Contains(first, "实际模型与请求不一致；工具调用超限") {
		t.Fatalf("first=%q", first)
	}
	for _, want := range []string{"kimi", "kimi-code/kimi-for-coding", "kimi-code/k3-256k", "未经独立核实", "done", "/tmp/l"} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q:\n%s", want, md)
		}
	}
}

func TestMarkdownIncludesMetadataAndLimitsFiles(t *testing.T) {
	r := Result{Status: StatusSucceeded, Harness: "codex", CLIVersion: "0.162.1", ModelRequested: "gpt-test", ModelActual: "gpt-test", ModelVerified: true, Access: "write", Duration: "2s", InputTokens: 100, OutputTokens: 20, CostUSD: 0.01, ToolCalls: 3, FinalText: "result\n```\nembedded fence", SessionID: "th-9", LogPath: "/tmp/log"}
	for i := 0; i < 21; i++ {
		r.Diff.Files = append(r.Diff.Files, FileDelta{Path: fmt.Sprintf("file-%02d", i), Added: 1})
	}
	r.Diff.Added = 21
	md := r.Markdown()
	if !strings.HasPrefix(md, "✓ codex 完成\n") {
		t.Fatalf("heading=%s", md)
	}
	for _, want := range []string{"0.162.1", "gpt-test", "write", "2s", "100", "20", "0.01", "3", "21", "+21", "file-19", "th-9", "/tmp/log", "result\n```\nembedded fence"} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "file-20") || strings.Contains(md, "未经独立核实") {
		t.Fatalf("unexpected content:\n%s", md)
	}
	if !strings.Contains(md, "````\nresult") {
		t.Fatalf("reply escaped its code fence:\n%s", md)
	}
}
