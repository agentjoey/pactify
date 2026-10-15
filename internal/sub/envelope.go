package sub

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type Result struct {
	JobID          string   `json:"jobId"`
	Status         Status   `json:"status"`
	Harness        string   `json:"harness"`
	CLIVersion     string   `json:"cliVersion"`
	ModelRequested string   `json:"modelRequested,omitempty"`
	ModelActual    string   `json:"modelActual,omitempty"`
	ModelVerified  bool     `json:"modelVerified"`
	Access         string   `json:"access"`
	Duration       string   `json:"duration"`
	InputTokens    int      `json:"inputTokens"`
	OutputTokens   int      `json:"outputTokens"`
	CostUSD        float64  `json:"costUSD,omitempty"`
	ToolCalls      int      `json:"toolCalls"`
	Diff           Diff     `json:"diff"`
	FinalText      string   `json:"finalText"`
	Truncated      bool     `json:"truncated"`
	SessionID      string   `json:"sessionId"`
	LogPath        string   `json:"logPath"`
	Warnings       []string `json:"warnings,omitempty"`
}

const FinalTextLimit = 4096

func TruncateFinal(s string) (string, bool) {
	if len(s) <= FinalTextLimit {
		return s, false
	}
	n := FinalTextLimit
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}

func (r Result) Markdown() string {
	var b strings.Builder
	if len(r.Warnings) > 0 {
		fmt.Fprintf(&b, "⚠️ %s\n", strings.Join(r.Warnings, "；"))
	} else if r.Status == StatusSucceeded {
		fmt.Fprintf(&b, "✓ %s 完成\n", r.Harness)
	} else {
		fmt.Fprintf(&b, "✗ %s %s\n", r.Harness, r.Status)
	}
	fmt.Fprintf(&b, "\nHarness: %s · CLI: %s\n", r.Harness, r.CLIVersion)
	requested := r.ModelRequested
	if requested == "" {
		requested = "（默认）"
	}
	fmt.Fprintf(&b, "模型: %s → %s", requested, r.ModelActual)
	if !r.ModelVerified {
		b.WriteString("（未经独立核实）")
	}
	fmt.Fprintf(&b, "\n权限: %s\n耗时: %s\nToken: 输入 %d · 输出 %d", r.Access, r.Duration, r.InputTokens, r.OutputTokens)
	if r.CostUSD != 0 {
		fmt.Fprintf(&b, " · 成本 $%.6f", r.CostUSD)
	}
	fmt.Fprintf(&b, "\n工具调用: %d\n改动: %d 个文件 · +%d/-%d\n", r.ToolCalls, len(r.Diff.Files), r.Diff.Added, r.Diff.Deleted)
	for i, f := range r.Diff.Files {
		if i == 20 {
			fmt.Fprintf(&b, "…另 %d 个文件\n", len(r.Diff.Files)-20)
			break
		}
		fmt.Fprintf(&b, "- %s (+%d/-%d)\n", f.Path, f.Added, f.Deleted)
	}
	// A longer fence preserves replies that contain their own Markdown fences.
	fence := "```"
	for strings.Contains(r.FinalText, fence) {
		fence += "`"
	}
	fmt.Fprintf(&b, "\n最终回复:\n%s\n%s\n%s\n", fence, r.FinalText, fence)
	if r.Truncated {
		b.WriteString("（最终回复已截断）\n")
	}
	fmt.Fprintf(&b, "\n会话: %s\n日志: %s\n", r.SessionID, r.LogPath)
	return b.String()
}
