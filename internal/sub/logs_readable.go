package sub

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/agentjoey/pactify/internal/cockpit"
)

type logEvent struct {
	cockpit.Event
	Time      string `json:"time"`
	Timestamp string `json:"timestamp"`
}

func parseLogEvent(line string) (logEvent, bool) {
	var event logEvent
	err := json.Unmarshal([]byte(line), &event)
	return event, err == nil && event.Kind != ""
}

// logRenderer emits display lines rather than physical records. Its message
// collector is scoped to one block: Final replaces its chunks, and tool start
// ends the ACP block before Collector resets those chunks.
type logRenderer struct {
	raw      bool
	message  Collector
	head     logEvent
	tools    map[string]*logTool
	lastTool string
}

type logTool struct {
	name, start, summary string
	output               strings.Builder
}

func (r *logRenderer) flushMessage(emit func(string) error) error {
	text := r.message.FinalText()
	event := r.head
	r.message = Collector{}
	r.head = logEvent{}
	if text == "" {
		return nil
	}
	event.Text = text
	return emit(formatLogEvent(event))
}

func (r *logRenderer) add(line string, emit func(string) error) error {
	if r.raw {
		return emit(logLine(line))
	}
	event, ok := parseLogEvent(line)
	if !ok {
		return emit(logLine(line))
	}
	switch event.Kind {
	case cockpit.EventMessage:
		if r.message.FinalText() == "" || (event.Final && event.Text != "") {
			r.head = event
		}
		r.message.Add(event.Event)
		if event.Final {
			return r.flushMessage(emit)
		}
		return nil
	case cockpit.EventTool:
		if event.Tool == nil {
			return emit(formatLogEvent(event))
		}
		if event.Tool.Phase == "start" {
			if err := r.flushMessage(emit); err != nil {
				return err
			}
		}
		return r.tool(event, emit)
	case cockpit.EventState:
		if event.State == cockpit.StateTurnCompleted || event.State == cockpit.StateTurnFailed {
			if err := r.flush(emit); err != nil {
				return err
			}
		}
	}
	return emit(formatLogEvent(event))
}

// Cockpit keeps tool identity and completion details in Raw. Codex uses item
// and itemId within a thread; ACP uses toolCallId, with terminal updates mapped
// to phase=output. Empty fields remain valid for harnesses without these details.
type logToolDetails struct {
	ThreadID   string `json:"threadId"`
	ItemID     string `json:"itemId"`
	ToolCallID string `json:"toolCallId"`
	Status     string `json:"status"`
	Content    []struct {
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"content"`
	Item struct {
		ID       string          `json:"id"`
		Status   string          `json:"status"`
		ExitCode *int            `json:"exitCode"`
		Output   string          `json:"aggregatedOutput"`
		Error    json.RawMessage `json:"error"`
	} `json:"item"`
}

func (r *logRenderer) tool(event logEvent, emit func(string) error) error {
	var details logToolDetails
	_ = json.Unmarshal(event.Raw, &details)
	id := details.Item.ID
	if id == "" {
		id = details.ItemID
	}
	if id == "" {
		id = details.ToolCallID
	}
	key := event.Tool.Name
	if id != "" {
		key = details.ThreadID + "/" + id
	}
	if key == "" {
		key = r.lastTool
	}
	if r.tools == nil {
		r.tools = map[string]*logTool{}
	}
	if event.Tool.Phase == "start" {
		r.tools[key] = &logTool{name: event.Tool.Name, start: event.Tool.Text}
		r.lastTool = key
		return emit(formatLogEvent(event))
	}
	tool := r.tools[key]
	if tool == nil {
		tool = &logTool{name: event.Tool.Name}
		r.tools[key] = tool
	}
	if event.Tool.Phase == "output" {
		tool.output.WriteString(event.Tool.Text)
		var content strings.Builder
		for _, part := range details.Content {
			content.WriteString(part.Content.Text)
		}
		if content.Len() > 0 {
			tool.summary = content.String()
		}
		if details.Status != "completed" && details.Status != "failed" {
			return nil
		}
	}
	summary := event.Tool.Text
	if summary == tool.start || summary == tool.name {
		summary = ""
	}
	// A terminal ACP output event has already joined its text above.
	if event.Tool.Phase == "output" {
		summary = ""
	}
	if details.Item.Output != "" {
		summary = details.Item.Output
	}
	if summary == "" {
		summary = tool.summary
	}
	if summary == "" {
		summary = tool.output.String()
	}
	reason := event.Err
	if reason == "" && len(details.Item.Error) > 0 && string(details.Item.Error) != "null" {
		var text string
		var object struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(details.Item.Error, &text) == nil {
			reason = text
		} else if json.Unmarshal(details.Item.Error, &object) == nil {
			reason = object.Message
		}
	}
	if reason == "" && details.Item.ExitCode != nil && *details.Item.ExitCode != 0 {
		reason = fmt.Sprintf("exit %d", *details.Item.ExitCode)
	}
	if reason == "" && (details.Status == "failed" || details.Item.Status == "failed") {
		reason = "failed"
	}
	if reason != "" {
		if summary != "" && summary != reason {
			summary = reason + ": " + summary
		} else {
			summary = reason
		}
	}
	delete(r.tools, key)
	if summary == "" {
		return nil
	}
	event.Tool = &cockpit.ToolEvent{Name: tool.name, Text: summary}
	return emit(formatLogEvent(event))
}

func (r *logRenderer) flush(emit func(string) error) error {
	if err := r.flushMessage(emit); err != nil {
		return err
	}
	keys := make([]string, 0, len(r.tools))
	for key := range r.tools {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		tool := r.tools[key]
		text := tool.summary
		if text == "" {
			text = tool.output.String()
		}
		if text != "" {
			event := logEvent{Event: cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Name: tool.name, Text: text}}}
			if err := emit(formatLogEvent(event)); err != nil {
				return err
			}
		}
		delete(r.tools, key)
	}
	return nil
}
