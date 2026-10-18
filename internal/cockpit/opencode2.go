package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Opencode2Rule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// Opencode2Permissions is also consumed by the non-billable permission probe.
func Opencode2Permissions(access Access) []Opencode2Rule {
	if access != AccessReadOnly {
		return []Opencode2Rule{{"edit", "*", "ask"}, {"shell", "*", "ask"}, {"question", "*", "deny"}}
	}
	// Upstream shell.ts:140-148 skips permission.assert when shell/parse.ts:173-198
	// produces no commands (for example, a redirect-only statement). Keep shell's
	// final rule as wildcard deny so whollyDisabled removes the tool entirely.
	// Tool removal: packages/core/src/tool.ts:231,292-295. Reassess the shell
	// whitelist only after the upstream bypass is fixed (docs/backlog.md, AJ-720 B1).
	return []Opencode2Rule{{"*", "*", "deny"}, {"read", "*", "allow"}, {"glob", "*", "allow"}, {"grep", "*", "allow"}, {"shell", "*", "deny"}}
}
func opencode2Permissions(access Access) []Opencode2Rule { return Opencode2Permissions(access) }

func Opencode2Env(opts StartOpts) ([]string, error) {
	env := append([]string{}, opts.Env...)
	content := os.Getenv("OPENCODE_CONFIG_CONTENT")
	for _, entry := range env {
		if v, ok := strings.CutPrefix(entry, "OPENCODE_CONFIG_CONTENT="); ok {
			content = v
		}
	}
	config := map[string]any{}
	if content != "" {
		if err := json.Unmarshal([]byte(content), &config); err != nil || config == nil {
			return nil, fmt.Errorf("invalid OPENCODE_CONFIG_CONTENT")
		}
	}
	delete(config, "permission")
	config["permissions"] = opencode2Permissions(opts.Access)
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	env = append(env, "OPENCODE_CONFIG_CONTENT="+string(raw))
	if opts.MinimalConfig {
		configHome := os.Getenv("XDG_CONFIG_HOME")
		for _, e := range env {
			if v, ok := strings.CutPrefix(e, "XDG_CONFIG_HOME="); ok {
				configHome = v
			}
		}
		if configHome != "" {
			env = append(env, "OPENCODE_CONFIG_DIR="+configHome+"/opencode", "OPENCODE_CONFIG=")
		}
		// OPENCODE_DISABLE_PROJECT_CONFIG would also stop AGENTS.md discovery
		// (server routes.ts), so project config stays enabled as in 1.x.
		env = append(env, "OPENCODE_DISABLE_AUTOUPDATE=1", "OPENCODE_DISABLE_CLAUDE_CODE=1")
		// Pin only the private database; user mode retains the native DB selection.
		env = append(env, "OPENCODE_DB=opencode.db", "OPENCODE_DISABLE_CHANNEL_DB=1")
	}
	return env, nil
}

type opencode2Model struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Enabled    bool   `json:"enabled"`
	Variants   []struct {
		ID string `json:"id"`
	} `json:"variants"`
}

func (s *opencodeServer) models2(ctx context.Context) ([]opencode2Model, error) {
	var models []opencode2Model
	err := s.call2(ctx, "GET", "/api/model", nil, &models)
	return models, err
}
func Opencode2Models(ctx context.Context, opts StartOpts) ([]string, error) {
	d, err := Opencode2ModelDetails(ctx, opts)
	return d.Items, err
}
func Opencode2ModelDetails(ctx context.Context, opts StartOpts) (ModelDiscovery, error) {
	s, err := startOpencode2Server(ctx, opts)
	if err != nil {
		return ModelDiscovery{}, err
	}
	defer s.close()
	models, err := s.models2(ctx)
	if err != nil {
		return ModelDiscovery{}, err
	}
	d := ModelDiscovery{Items: []string{}}
	efforts := map[string]bool{}
	for _, m := range models {
		if !m.Enabled {
			continue
		}
		d.Items = append(d.Items, m.ProviderID+"/"+m.ID)
		for _, v := range m.Variants {
			efforts[v.ID] = true
		}
	}
	for e := range efforts {
		d.Efforts = append(d.Efforts, e)
	}
	sort.Strings(d.Items)
	sort.Strings(d.Efforts)
	if len(d.Items) == 0 {
		return d, fmt.Errorf("opencode returned no models")
	}
	return d, nil
}

var opencode2ResetTime = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`)

func opencode2Failure(raw json.RawMessage) (Failure, bool) {
	var e struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &e) != nil || !(strings.HasPrefix(e.Type, "provider.") || strings.HasPrefix(e.Type, "pactify.") || strings.HasPrefix(e.Type, "permission.") || strings.HasPrefix(e.Type, "tool.")) {
		return Failure{}, false
	}
	class := "other"
	switch e.Type {
	case "pactify.canceled":
		class = "canceled"
	case "provider.auth":
		class = "auth"
	case "provider.quota":
		class = "quota"
	case "provider.rate-limit", "provider.internal", "provider.transport", "provider.timeout", "provider.unknown":
		class = "transient"
	case "provider.no-route":
		class = "model"
	}
	f := Failure{Class: class}
	if class == "quota" {
		if stamp := opencode2ResetTime.FindString(string(raw)); stamp != "" {
			if reset, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
				f.ResetAt = &reset
			}
		}
	}
	return f, true
}
func opencode2ErrorText(raw json.RawMessage) string {
	var e struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	if e.Message == "" {
		e.Message = "Opencode session failed"
	}
	if e.Type == "provider.auth" {
		e.Message += "; run pactify sub login opencode"
	}
	return e.Message
}
