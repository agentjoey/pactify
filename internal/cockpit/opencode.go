package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// OpencodeEnv applies explicit sub permissions after any inherited process-local
// config. A malformed config cannot be safely merged, so fail before spawning.
func OpencodeEnv(opts StartOpts) ([]string, error) {
	env := append([]string(nil), opts.Env...)
	if opts.Access != AccessDefault {
		content := os.Getenv("OPENCODE_CONFIG_CONTENT")
		for _, entry := range env {
			if value, ok := strings.CutPrefix(entry, "OPENCODE_CONFIG_CONTENT="); ok {
				content = value
			}
		}
		config := map[string]any{}
		if content != "" {
			if err := json.Unmarshal([]byte(content), &config); err != nil || config == nil {
				return nil, fmt.Errorf("invalid OPENCODE_CONFIG_CONTENT")
			}
		}
		permission := map[string]any{"edit": "allow", "bash": "allow", "webfetch": "allow"}
		if opts.Access == AccessReadOnly {
			permission = map[string]any{"*": "deny", "edit": "deny", "write": "deny", "read": "allow", "glob": "allow", "grep": "allow", "list": "allow", "bash": "deny"}
			config["experimental"] = map[string]any{"continue_loop_on_deny": true}
		}
		config["permission"] = permission
		raw, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		env = append(env, "OPENCODE_CONFIG_CONTENT="+string(raw))
	}
	if opts.MinimalConfig {
		env = append(env, "OPENCODE_DISABLE_AUTOUPDATE=1", "OPENCODE_DISABLE_CLAUDE_CODE=1", "OPENCODE_DISABLE_EXTERNAL_SKILLS=1", "OPENCODE_PURE=1")
	}
	return env, nil
}

// OpencodeModels discovers public model IDs over v1 without submitting a prompt.
func OpencodeModels(ctx context.Context, opts StartOpts) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	server, err := startOpencodeServer(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer server.close()
	return server.models(ctx)
}

func opencodeFailure(message string, raw json.RawMessage) Failure {
	if f, ok := opencode2Failure(raw); ok {
		return f
	}
	var e struct {
		Name string `json:"name"`
		Data struct {
			Message     string `json:"message"`
			StatusCode  int    `json:"statusCode"`
			IsRetryable bool   `json:"isRetryable"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &e) == nil {
		if e.Name == "ProviderAuthError" || e.Name == "APIError" && (e.Data.StatusCode == 401 || e.Data.StatusCode == 403) {
			return Failure{Class: "auth"}
		}
	}
	if e.Name == "APIError" {
		text := strings.ToLower(message + " " + e.Data.Message)
		if e.Data.StatusCode == 429 {
			for _, word := range []string{"balance", "quota", "credit", "budget", "usage limit", "limit reached", "余额", "额度"} {
				if strings.Contains(text, word) {
					return Failure{Class: "quota"}
				}
			}
			return Failure{Class: "transient"}
		}
		if e.Data.IsRetryable || e.Data.StatusCode >= 500 && e.Data.StatusCode <= 599 {
			return Failure{Class: "transient"}
		}
	}
	// Historical results can still carry stopReason or the old balance text.
	var stop struct {
		Reason string `json:"stopReason"`
	}
	if json.Unmarshal(raw, &stop) == nil && stop.Reason != "" {
		return acpFailure(message, raw, nil)
	}
	return matchFailure(message, []failureRule{{"opencode 2.x has no pactify login", "auth"}, {"run opencode login first", "auth"}, {"insufficient balance", "quota"}, {"requested model is not offered by this agent", "model"}})
}
