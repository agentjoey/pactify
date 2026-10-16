package cockpit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/agentjoey/pactify/internal/acp"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A fake ACP executable. It never opens user configuration or credentials.
func TestOpencodeProcessHelper(t *testing.T) {
	if os.Getenv("COCKPIT_OPENCODE_HELPER") != "1" {
		return
	}
	options := []map[string]any{
		{"id": "model", "currentValue": "test/default", "options": []map[string]string{{"value": "test/default"}, {"value": "test/flash"}}},
		{"id": "effort", "currentValue": "default", "options": []map[string]string{{"value": "low"}, {"value": "default"}}},
		{"id": "mode", "currentValue": "plan", "options": []map[string]string{{"value": "build"}, {"value": "plan"}}},
	}
	scan := bufio.NewScanner(os.Stdin)
	cancelled := false
	for scan.Scan() {
		var req struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &req) != nil {
			os.Exit(2)
		}
		if req.Method == "session/cancel" {
			cancelled = true
			continue
		}
		if req.ID == nil {
			continue
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]bool{"loadSession": true}}
		case "session/new", "session/load":
			if req.Params["cwd"] == "" || req.Params["mcpServers"] == nil {
				os.Exit(3)
			}
			result = map[string]any{"sessionId": "sid", "configOptions": options}
		case "session/set_mode":
			os.Exit(4) // opencode uses configOptions, never plan as a sandbox.
		case "session/set_config_option":
			for _, o := range options {
				if o["id"] == req.Params["configId"] {
					o["currentValue"] = req.Params["value"]
				}
			}
			result = map[string]any{"configOptions": options}
		case "session/prompt":
			var config map[string]any
			if json.Unmarshal([]byte(os.Getenv("OPENCODE_CONFIG_CONTENT")), &config) != nil {
				os.Exit(5)
			}
			text := fmt.Sprintf("%v|%v|%v|%v|%s|%s|%s", options[0]["currentValue"], options[1]["currentValue"], options[2]["currentValue"], config["permission"], os.Getenv("OPENCODE_DISABLE_CLAUDE_CODE"), os.Getenv("OPENCODE_DISABLE_EXTERNAL_SKILLS"), os.Getenv("OPENCODE_PURE"))

			mustWriteJSON(os.Stdout, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "sid", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": text}}}})
			for _, cost := range []float64{0.2, 0.5} {
				mustWriteJSON(os.Stdout, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "sid", "update": map[string]any{"sessionUpdate": "usage_update", "used": 999, "size": 10000, "cost": map[string]any{"amount": cost, "currency": "USD"}}}})
			}
			stop := "end_turn"
			if cancelled {
				stop = "cancelled"
			}
			result = map[string]any{"stopReason": stop, "usage": map[string]int{"inputTokens": 100, "cachedReadTokens": 200, "outputTokens": 7, "totalTokens": 307, "thoughtTokens": 4}}
		}
		mustWriteJSON(os.Stdout, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	os.Exit(0)
}

func fakeOpencodeProcess(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("COCKPIT_OPENCODE_HELPER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "' -test.run='^TestOpencodeProcessHelper$' -- \"$@\"\n"
	if err = os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestOpencodeACPConfigurationUsageResume(t *testing.T) {
	fakeOpencodeProcess(t)
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"model":"keep/model","permission":{"*":"deny"},"other":true}`)
	for _, resume := range []bool{false, true} {
		for _, access := range []Access{AccessWrite} {
			for _, model := range []string{"", "test/flash"} {
				t.Run(fmt.Sprintf("%v/%s/%s", resume, access, model), func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					opts := StartOpts{RepoDir: t.TempDir(), Access: access, Model: model, Effort: "low", MinimalConfig: true}
					b := NewACPBackend("opencode")
					var s Session
					var err error
					if resume {
						s, err = b.(OptsResumer).ResumeWith(ctx, "sid", opts)
					} else {
						s, err = b.Start(ctx, opts)
					}
					if err != nil {
						t.Fatal(err)
					}
					defer s.Close()
					if err = s.Prompt(ctx, UserMessage{Text: "test"}); err != nil {
						t.Fatal(err)
					}
					var text string
					var usage Usage
					for e := range s.Events() {
						if e.Kind == EventMessage {
							text += e.Text
						}
						if e.Usage != nil {
							usage = *e.Usage
						}
						if e.State == StateTurnCompleted {
							break
						}
					}
					wantModel := model
					if wantModel == "" {
						wantModel = "test/default"
					}
					permission := "bash:allow edit:allow webfetch:allow"
					if !strings.HasPrefix(text, wantModel+"|low|build|map["+permission+"]|1|1|1") {
						t.Fatalf("configuration=%s", text)
					}
					if usage.InputTokens != 300 || usage.OutputTokens != 7 || usage.CostUSD != 0.5 {
						t.Fatalf("usage=%+v", usage)
					}
					if got := s.(InfoSession).Info(); got.Model != wantModel || got.Effort != "low" || got.Access != "build" {
						t.Fatalf("info=%+v", got)
					}
				})
			}
		}
	}
}

func TestOpencodeCancellation(t *testing.T) {
	fakeOpencodeProcess(t)
	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s, err := NewACPBackend("opencode").Start(ctx, StartOpts{RepoDir: t.TempDir(), Access: AccessWrite})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if err = s.Interrupt(ctx); err != nil {
			t.Fatal(err)
		}
		if err = s.Prompt(ctx, UserMessage{Text: "cancelled"}); err != nil {
			t.Fatal(err)
		}
		for e := range s.Events() {
			if e.State == StateTurnFailed {
				if e.Err != "opencode cancelled" {
					t.Fatal(e)
				}
				break
			}
		}
	})
}

func TestOpencodeEnvMergeAndUserConfig(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"permission":{"*":"deny"},"model":"test/default","other":true}`)
	for _, minimal := range []bool{true, false} {
		env, err := OpencodeEnv(StartOpts{Access: AccessWrite, MinimalConfig: minimal, Env: []string{`OPENCODE_CONFIG_CONTENT={"plugin":[],"permission":{"edit":"allow"}}`}})
		if err != nil {
			t.Fatal(err)
		}
		var config map[string]any
		for _, e := range env {
			if v, ok := strings.CutPrefix(e, "OPENCODE_CONFIG_CONTENT="); ok {
				json.Unmarshal([]byte(v), &config)
			}
		}
		if config["plugin"] == nil || config["permission"].(map[string]any)["edit"] != "allow" || config["other"] != nil {
			t.Fatal(config)
		}
		if strings.Contains(strings.Join(env, "\n"), "OPENCODE_PURE=1") != minimal {
			t.Fatal(env)
		}
	}
	t.Setenv("OPENCODE_CONFIG_CONTENT", "not-json")
	if _, err := OpencodeEnv(StartOpts{Access: AccessWrite}); err == nil {
		t.Fatal("malformed config accepted")
	}
}

func TestOpencodeDynamicModelsAndUnknownModel(t *testing.T) {
	fakeOpencodeProcess(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := OpencodeModels(ctx, StartOpts{RepoDir: t.TempDir(), MinimalConfig: true})
	if err != nil || strings.Join(models, ",") != "test/default,test/flash" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	_, err = NewACPBackend("opencode").Start(ctx, StartOpts{RepoDir: t.TempDir(), Access: AccessWrite, Model: "missing/model"})
	if err == nil || !strings.Contains(err.Error(), "test/flash") {
		t.Fatalf("unknown model error=%v", err)
	}
}

type opencodeModeReset struct{ fakeConfigurer }

func (f *opencodeModeReset) SetConfigOption(ctx context.Context, sid acp.SessionID, id, value string) ([]acp.ConfigOption, error) {
	options, err := f.fakeConfigurer.SetConfigOption(ctx, sid, id, value)
	if id == "mode" {
		options = append(options, acp.ConfigOption{ID: "model", CurrentValue: "test/wrong"})
	}
	return options, err
}
func TestOpencodeModeMustNotResetRequestedModel(t *testing.T) {
	res := acp.NewSessionResult{ConfigOptions: []acp.ConfigOption{
		{ID: "model", CurrentValue: "test/default", Options: []acp.ConfigValue{{Value: "test/flash"}}},
		{ID: "mode", CurrentValue: "plan", Options: []acp.ConfigValue{{Value: "build"}}},
	}}
	_, err := applyOpencodeOpts(context.Background(), &opencodeModeReset{}, "sid", res, StartOpts{Model: "test/flash", Access: AccessWrite})
	if !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("model reset accepted: %v", err)
	}
}

func TestOpencodeDoesNotDropFinalUsageBehindOutput(t *testing.T) {
	s := configuredACPSession(nil, "sid", "opencode")
	defer s.Close()
	for i := 0; i < cap(s.events); i++ {
		s.dispatchEvent(Event{Kind: EventMessage, Text: "chunk"})
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.opencodeUpdate(acp.SessionUpdate{Kind: "usage", Usage: &acp.Usage{InputTokens: 10, CachedReadTokens: 20, OutputTokens: 3}})
	}()
	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
	}
	timeout := time.After(time.Second)
	for {
		select {
		case e := <-s.Events():
			if e.Usage != nil {
				if e.Usage.InputTokens != 30 || e.Usage.OutputTokens != 3 {
					t.Fatal(e.Usage)
				}
				<-done
				return
			}
		case <-timeout:
			t.Fatal("final usage was dropped behind output")
		}
	}
}
