package sub

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/agentjoey/pactify/internal/cockpit"
)

// A strict projection is the API boundary: Raw may contain credentials and is
// never forwarded. Adapter redaction has already run before log persistence.
// Also mask common credential assignments in display text from older logs.
var displayCredential = regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*["']?(?:bearer\s+|basic\s+)?|(?:api[_-]?key|(?:access[_-]?|refresh[_-]?|auth[_-]?)?token|password|secret)["']?\s*[:=]\s*["']?)([^\s"',;]+)`)

var displayCredentialFlag = regexp.MustCompile(`(?i)(--(?:api[_-]?key|(?:access[_-]?|refresh[_-]?|auth[_-]?)?token|password|secret)(?:=|\s+)["']?)([^\s"',;]+)`)

func safeEventText(s string) string {
	s = displayCredential.ReplaceAllString(s, "${1}[redacted]")
	return singleLogLine(displayCredentialFlag.ReplaceAllString(s, "${1}[redacted]"))
}

type liveTool struct {
	Phase  string `json:"phase"`
	Name   string `json:"name"`
	Text   string `json:"text,omitempty"`
	ID     string `json:"id,omitempty"`
	Type   string `json:"type,omitempty"`
	Status string `json:"status,omitempty"`
}
type liveEvent struct {
	Kind      cockpit.EventKind `json:"kind"`
	Time      string            `json:"time,omitempty"`
	Text      string            `json:"text,omitempty"`
	Final     bool              `json:"final,omitempty"`
	State     string            `json:"state,omitempty"`
	Tool      *liveTool         `json:"tool,omitempty"`
	Usage     *liveUsage        `json:"usage,omitempty"`
	CostUSD   *float64          `json:"costUSD,omitempty"`
	RateLimit string            `json:"rateLimit,omitempty"`
	Diff      *liveDiff         `json:"diff,omitempty"`
}
type liveUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

type liveDiff struct {
	Files   int `json:"files"`
	Added   int `json:"added"`
	Deleted int `json:"deleted"`
}

func (r *logRenderer) project(line string, emit func(string) error) error {
	e, ok := parseLogEvent(line)
	if !ok {
		return nil
	}
	v := liveEvent{Kind: e.Kind, Time: safeEventText(e.Time), Final: e.Final}
	if v.Time == "" {
		v.Time = safeEventText(e.Timestamp)
	}
	switch e.Kind {
	case cockpit.EventMessage:
		v.Text = safeEventText(e.Text)
	case cockpit.EventState:
		v.State = e.State
		if e.State != "turn_started" && e.State != "turn_completed" && e.State != "turn_failed" {
			return nil
		}
	case cockpit.EventTool:
		if e.Tool == nil {
			return nil
		}
		var raw struct {
			ThreadID   string `json:"threadId"`
			ItemID     string `json:"itemId"`
			ToolCallID string `json:"toolCallId"`
			Kind       string `json:"kind"`
			Status     string `json:"status"`
			Title      string `json:"title"`
			RawInput   struct {
				Command string `json:"command"`
			} `json:"rawInput"`
			Item struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"item"`
			Part struct {
				ID    string `json:"id"`
				State struct {
					Status string `json:"status"`
					Input  struct {
						Command string `json:"command"`
					} `json:"input"`
				} `json:"state"`
			} `json:"part"`
		}
		_ = json.Unmarshal(e.Raw, &raw)
		t := &liveTool{Phase: e.Tool.Phase, Name: safeEventText(e.Tool.Name), Text: safeEventText(e.Tool.Text)}
		id := raw.Item.ID
		if id == "" {
			id = raw.ItemID
		}
		if id == "" {
			id = raw.ToolCallID
		}
		if id == "" {
			id = raw.Part.ID
		}
		if id != "" {
			t.ID = safeEventText(raw.ThreadID + "/" + id)
		}
		// Only known type/status enums cross the boundary.
		for _, kind := range []string{raw.Item.Type, raw.Kind} {
			switch kind {
			case "commandExecution", "execute":
				t.Type = "shell"
			case "fileChange", "edit":
				t.Type = "edit"
			case "read":
				t.Type = "read"
			}
		}
		for _, status := range []string{raw.Status, raw.Part.State.Status} {
			switch status {
			case "completed", "failed", "in_progress", "running":
				t.Status = status
			}
		}
		if t.Text == "" {
			t.Text = safeEventText(raw.RawInput.Command)
		}
		if t.Text == "" {
			t.Text = safeEventText(raw.Part.State.Input.Command)
		}
		if t.Name == "" {
			t.Name = safeEventText(raw.Title)
		}
		v.Tool = t
	case cockpit.EventUsage:
		r.collector.Add(e.Event)
		if e.Usage == nil && r.collector.threadTotals == nil {
			return nil
		}
		u := r.collector.Usage
		// A zero default or cumulative ACP session cost is not a known job price.
		v.Usage = &liveUsage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens}
		if r.collector.JobUsageDelta && r.collector.Usage.CostUSD > 0 {
			c := r.collector.Usage.CostUSD
			v.CostUSD = &c
		}
	case cockpit.EventSystem:
		if e.State == "transient_retry" || e.State == "empty_reply_followup" {
			r.collector.Add(e.Event)
			return nil
		}
		if e.State != "rate_limits" {
			return nil
		}
		v.State = "rate_limits"
		v.RateLimit = SummarizeRateLimit(e.Raw)
		if v.RateLimit == "" {
			return nil
		}
	case cockpit.EventDiff:
		var raw struct {
			Diff *string `json:"diff"`
		}
		if json.Unmarshal(e.Raw, &raw) != nil || raw.Diff == nil {
			return nil
		}
		d := &liveDiff{}
		inHunk := false
		for _, l := range strings.Split(*raw.Diff, "\n") {
			switch {
			case strings.HasPrefix(l, "diff --git "):
				d.Files++
				inHunk = false
			case strings.HasPrefix(l, "@@"):
				inHunk = true
			case !inHunk:
			case strings.HasPrefix(l, "+"):
				d.Added++
			case strings.HasPrefix(l, "-"):
				d.Deleted++
			}
		}
		v.Diff = d
	default:
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return emit(string(b))
}
