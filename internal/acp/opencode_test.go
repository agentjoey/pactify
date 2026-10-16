package acp

import (
	"encoding/json"
	"testing"
)

func TestLoadSessionParameters(t *testing.T) {
	server := &configServer{onMethod: map[string]func(*fakeServer, json.RawMessage) any{
		"session/load": func(_ *fakeServer, raw json.RawMessage) any {
			var params map[string]any
			if err := json.Unmarshal(raw, &params); err != nil {
				t.Fatal(err)
			}
			if params["sessionId"] != "old-session" || params["cwd"] != "." || len(params["mcpServers"].([]any)) != 0 {
				t.Errorf("params=%s", raw)
			}
			return json.RawMessage(`{"configOptions":[{"id":"model","currentValue":"provider/default"},{"id":"effort","currentValue":"default"},{"id":"mode","currentValue":"build"}]}`)
		},
	}}
	c := newConfigTestClient(t, server)
	c.mu.Lock()
	c.loadSession = true
	c.mu.Unlock()
	if err := c.LoadSession(ctx(t), "old-session"); err != nil {
		t.Fatal(err)
	}
}

func TestPromptCachedReadUsage(t *testing.T) {
	c := newConfigTestClient(t, &configServer{onMethod: map[string]func(*fakeServer, json.RawMessage) any{
		"session/prompt": func(_ *fakeServer, _ json.RawMessage) any {
			return json.RawMessage(`{"stopReason":"end_turn","usage":{"inputTokens":10,"cachedReadTokens":20,"outputTokens":3,"thoughtTokens":2}}`)
		},
	}})
	var usage *Usage
	c.OnSessionUpdateFor("sid", func(u SessionUpdate) { usage = u.Usage })
	_, err := c.Prompt(ctx(t), "sid", "test")
	if err != nil || usage == nil || usage.InputTokens != 10 || usage.CachedReadTokens != 20 || usage.OutputTokens != 3 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
}
