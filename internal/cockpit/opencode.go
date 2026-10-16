package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
)

// OpencodeNoReadOnly explains the disabled access mode at both routing and backend boundaries.
const OpencodeNoReadOnly = "Read-only access is not offered for opencode in the first release: all 3 evaluation read-only tasks ended without a conclusion after shell requests were denied. Use codex --read-only for read-only tasks; use opencode --write only with explicit write permission"

// OpencodeEnv applies explicit sub permissions after any inherited process-local
// config. A malformed config cannot be safely merged, so fail before spawning.
func OpencodeEnv(opts StartOpts) ([]string, error) {
	if opts.Access == AccessReadOnly {
		return nil, fmt.Errorf("%s", OpencodeNoReadOnly)
	}
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
		permission := map[string]string{"edit": "allow", "bash": "allow", "webfetch": "allow"}
		config["permission"] = permission
		raw, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		env = append(env, "OPENCODE_CONFIG_CONTENT="+string(raw))
	}
	if opts.MinimalConfig {
		env = append(env, "OPENCODE_DISABLE_CLAUDE_CODE=1", "OPENCODE_DISABLE_EXTERNAL_SKILLS=1", "OPENCODE_PURE=1")
	}
	return env, nil
}

func applyOpencodeOpts(ctx context.Context, c acpConfigurer, sid acp.SessionID, res acp.NewSessionResult, opts StartOpts) (SessionInfo, error) {
	// Reuse model/effort confirmation without kimi's plan-mode permission mapping.
	for i := range res.ConfigOptions {
		if res.ConfigOptions[i].ID == "effort" {
			res.ConfigOptions[i].Category = "thought_level"
		}
	}
	modelOpts := opts
	modelOpts.Access = AccessDefault
	info, err := applyACPOptsFor(ctx, opencodeConfigurer{c}, sid, res, modelOpts, "opencode")
	if err != nil {
		return info, err
	}
	if opts.Access != AccessDefault {
		mode, ok := acp.FindConfig(res.ConfigOptions, "mode")
		if !ok || !offers(mode, "build") {
			return info, fmt.Errorf("opencode does not advertise build mode")
		}
		updated, err := c.SetConfigOption(ctx, sid, mode.ID, "build")
		if err != nil {
			return info, fmt.Errorf("set build mode: %w", err)
		}
		actual, ok := acp.FindConfig(updated, mode.ID)
		if !ok || actual.CurrentValue != "build" {
			return info, fmt.Errorf("opencode did not confirm build mode")
		}
		info.Access = "build"
		if model, ok := acp.FindConfig(updated, "model"); ok {
			info.Model = model.CurrentValue
			if opts.Model != "" && info.Model != opts.Model {
				return info, fmt.Errorf("%w: asked for %q, reported %q after setting build mode", ErrModelUnavailable, opts.Model, info.Model)
			}
		}
	}
	return info, nil
}

// OpencodeModels asks ACP for the live list without issuing a prompt.
func OpencodeModels(ctx context.Context, opts StartOpts) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	env, err := OpencodeEnv(opts)
	if err != nil {
		return nil, err
	}
	c, err := acp.Spawn(ctx, "opencode", []string{"acp"}, env, opts.RepoDir, opts.Stderr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if _, err = c.Initialize(ctx); err != nil {
		return nil, err
	}
	res, err := c.NewSessionDetailed(ctx, opts.RepoDir)
	if err != nil {
		return nil, err
	}
	model, ok := acp.FindConfig(res.ConfigOptions, "model")
	if !ok {
		return nil, ErrModelUnsupported
	}
	return values(model), nil
}

type opencodeConfigurer struct{ acpConfigurer }

func (c opencodeConfigurer) SetConfigOption(ctx context.Context, sid acp.SessionID, id, value string) ([]acp.ConfigOption, error) {
	options, err := c.acpConfigurer.SetConfigOption(ctx, sid, id, value)
	for i := range options {
		if options[i].ID == "effort" {
			options[i].Category = "thought_level"
		}
	}
	return options, err
}

var opencodeFailureRules = []failureRule{
	{"insufficient balance", "quota"},
	{"acp: rpc error -32000:", "auth"},
	{"requested model is not offered by this agent", "model"},
	{"opencode cancelled", "canceled"},
}
