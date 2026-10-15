package sub

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type Result struct {
	ConfigMode     string   `json:"configMode"`
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
	RateLimit      string   `json:"rateLimit,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
	WorktreePath   string   `json:"worktreePath,omitempty"`
	BaseCommit     string   `json:"baseCommit,omitempty"`
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
		fmt.Fprintf(&b, "⚠️ %s\n", strings.Join(r.Warnings, "; "))
	} else if r.Status == StatusSucceeded {
		fmt.Fprintf(&b, "✓ %s succeeded\n", r.Harness)
	} else {
		fmt.Fprintf(&b, "✗ %s %s\n", r.Harness, r.Status)
	}
	if r.ConfigMode == "" {
		// Keep historical results' rendering; new jobs always record their mode.
		fmt.Fprintf(&b, "\nHarness: %s · CLI: %s\n", r.Harness, r.CLIVersion)
	} else {
		fmt.Fprintf(&b, "\nHarness: %s (%s config) · CLI: %s\n", r.Harness, r.ConfigMode, r.CLIVersion)
	}
	requested := r.ModelRequested
	if requested == "" {
		requested = "(default)"
	}
	fmt.Fprintf(&b, "Model: %s → %s", requested, r.ModelActual)
	if !r.ModelVerified {
		b.WriteString(" (unverified)")
	}
	fmt.Fprintf(&b, "\nAccess: %s\nDuration: %s\nTokens: in %d · out %d", r.Access, r.Duration, r.InputTokens, r.OutputTokens)
	if r.CostUSD != 0 {
		fmt.Fprintf(&b, " · Cost $%.6f", r.CostUSD)
	}
	if r.RateLimit != "" {
		fmt.Fprintf(&b, "\nQuota: %s", r.RateLimit)
	}
	files := "files"
	if len(r.Diff.Files) == 1 {
		files = "file"
	}
	fmt.Fprintf(&b, "\nTool calls: %d\nChanges: %d %s · +%d/-%d\n", r.ToolCalls, len(r.Diff.Files), files, r.Diff.Added, r.Diff.Deleted)
	if r.WorktreePath != "" {
		fmt.Fprintf(&b, "Worktree: %s\nBase commit: %s\n", r.WorktreePath, r.BaseCommit)
	}
	for i, f := range r.Diff.Files {
		if i == 20 {
			fmt.Fprintf(&b, "…more files: %d\n", len(r.Diff.Files)-20)
			break
		}
		fmt.Fprintf(&b, "- %s (+%d/-%d)\n", f.Path, f.Added, f.Deleted)
	}
	// A longer fence preserves replies that contain their own Markdown fences.
	fence := "```"
	for strings.Contains(r.FinalText, fence) {
		fence += "`"
	}
	fmt.Fprintf(&b, "\nFinal reply:\n%s\n%s\n%s\n", fence, r.FinalText, fence)
	if r.Truncated {
		b.WriteString("(Final reply truncated)\n")
	}
	fmt.Fprintf(&b, "\nSession: %s\nLog: %s\n", r.SessionID, r.LogPath)
	return b.String()
}

// rateWindow is one window of codex's account/rateLimits/updated payload.
type rateWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins int     `json:"windowDurationMins"`
	ResetsAt           int64   `json:"resetsAt"`
}

// SummarizeRateLimit renders codex's rate-limit payload as one line, e.g.
// "7-day window: 10% used, resets 10-22 13:38"; empty when there is none.
func SummarizeRateLimit(raw []byte) string {
	var p struct {
		RateLimits struct {
			Primary   *rateWindow `json:"primary"`
			Secondary *rateWindow `json:"secondary"`
		} `json:"rateLimits"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &p) != nil {
		return ""
	}
	var parts []string
	for _, w := range []*rateWindow{p.RateLimits.Primary, p.RateLimits.Secondary} {
		if w == nil {
			continue
		}
		window := fmt.Sprintf("%d-minute", w.WindowDurationMins)
		if w.WindowDurationMins%1440 == 0 {
			window = fmt.Sprintf("%d-day", w.WindowDurationMins/1440)
		} else if w.WindowDurationMins%60 == 0 {
			window = fmt.Sprintf("%d-hour", w.WindowDurationMins/60)
		}
		part := fmt.Sprintf("%s window: %.0f%% used", window, w.UsedPercent)
		if w.ResetsAt > 0 {
			part += ", resets " + time.Unix(w.ResetsAt, 0).Local().Format("01-02 15:04")
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}
