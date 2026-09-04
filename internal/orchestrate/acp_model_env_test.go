package orchestrate

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/roles"
)

// envValue returns the last value for key in a child env slice (last wins, the
// same rule os/exec applies).
func envValue(env []string, key string) (string, bool) {
	val, found := "", false
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			val, found = strings.TrimPrefix(kv, key+"="), true
		}
	}
	return val, found
}

// [ACP-MODEL] root fix for opencode. Verified end-to-end against a real
// `opencode acp` server before writing this: with no override the session runs
// providerID=deepseek modelID=deepseek-v4-pro (the reported bug); with
// OPENCODE_CONFIG_CONTENT={"model":"minimax/MiniMax-M3"} the same session runs
// providerID=minimax modelID=MiniMax-M3. `opencode debug config` confirms the
// value MERGES into the resolved config — provider/mcp/agent/permission/plugin
// stay byte-identical — so pinning a model cannot clobber the user's setup.
func TestAcpInjectsModelPinForOpencode(t *testing.T) {
	bindSeatProfile(t, "w1", "mm", roles.Profile{Kind: "opencode", Model: "minimax/MiniMax-M3"})

	var cap acpLaunch
	r := AcpRunner{Spawn: captureSpawn(okStint(), &cap), Warn: func(string) {}}

	if err := r.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "opencode", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	raw, ok := envValue(cap.env, "OPENCODE_CONFIG_CONTENT")
	if !ok {
		t.Fatalf("opencode ACP child must carry the model pin in OPENCODE_CONFIG_CONTENT, env: %v", cap.env)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("OPENCODE_CONFIG_CONTENT must be valid JSON, got %q: %v", raw, err)
	}
	if cfg["model"] != "minimax/MiniMax-M3" {
		t.Errorf("model = %v, want minimax/MiniMax-M3", cfg["model"])
	}
}

// A pin that CAN be honored must not also warn — the warning exists to flag a
// silently dropped pin, and firing it here would train operators to ignore it.
func TestAcpDoesNotWarnWhenThePinIsHonored(t *testing.T) {
	bindSeatProfile(t, "w1", "mm", roles.Profile{Kind: "opencode", Model: "minimax/MiniMax-M3"})

	var warnings []string
	r := AcpRunner{Spawn: captureSpawn(okStint(), nil), Warn: func(m string) { warnings = append(warnings, m) }}

	if err := r.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "opencode", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("honored pin must not warn, got: %v", warnings)
	}
}

// Kinds with no known ACP model channel keep the old behavior: the pin really is
// dropped, so it must still say so.
func TestAcpStillWarnsForKindWithNoModelChannel(t *testing.T) {
	bindSeatProfile(t, "w1", "kk", roles.Profile{Kind: "kimi-cli", Model: "kimi-k2.5"})

	var warnings []string
	var cap acpLaunch
	r := AcpRunner{Spawn: captureSpawn(okStint(), &cap), Warn: func(m string) { warnings = append(warnings, m) }}

	if err := r.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "kimi-cli", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("want the dropped-pin warning, got %v", warnings)
	}
	if _, ok := envValue(cap.env, "OPENCODE_CONFIG_CONTENT"); ok {
		t.Error("must not leak an opencode-specific variable into another vendor's child")
	}
}

// No pin, no injection: an unbound opencode seat keeps the user's own default.
func TestAcpInjectsNothingWithoutAPin(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())

	var cap acpLaunch
	r := AcpRunner{Spawn: captureSpawn(okStint(), &cap), Warn: func(string) {}}

	if err := r.Run(context.Background(), LaunchContext{Seat: "w1", Kind: "opencode", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if v, ok := envValue(cap.env, "OPENCODE_CONFIG_CONTENT"); ok {
		t.Errorf("unbound seat must inherit opencode's own default, got %q", v)
	}
}
