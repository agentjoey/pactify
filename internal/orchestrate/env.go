package orchestrate

import (
	"os"
	"strings"
)

// filteredEnviron returns the current process environment with pactify/relay
// internal secrets removed. We use a denylist (drop known pactify keys) rather
// than a strict whitelist because vendor authentication depends on a wide set
// of system and HOME-directory variables that vary by agent. The goal is to
// avoid leaking pactify/relay secrets to third-party npx bridges, not to
// minimize the child's environment.
func filteredEnviron() []string {
	var out []string
	for _, e := range os.Environ() {
		if key, _, ok := strings.Cut(e, "="); ok {
			if key == "PACT_RELAY_TOKEN" || strings.HasPrefix(key, "PACTIFY_") {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// allVendorAPIKeys is every third-party model-provider credential pactify may
// carry in its environment. A dedicated single-vendor agent has no business
// reading a sibling vendor's key — e.g. a kimi/gemini/codex stint inheriting the
// user's real ANTHROPIC_API_KEY (review finding M11) — so when launching one we
// blank the keys that aren't its own. os/exec keeps the last value for a
// duplicate key, so a trailing `KEY=` overrides the inherited one.
var allVendorAPIKeys = []string{
	"ANTHROPIC_API_KEY",
	"OPENAI_API_KEY",
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"MOONSHOT_API_KEY",
	"KIMI_API_KEY",
}

// vendorOwnKeys maps a single-vendor agent kind to the API keys that ARE its own.
// A kind absent here (opencode, custom kinds) is model-agnostic — it may
// legitimately use any provider — so its environment is left untouched.
var vendorOwnKeys = map[string][]string{
	"claude-code": {"ANTHROPIC_API_KEY"},
	"codex-cli":   {"OPENAI_API_KEY"},
	"gemini-cli":  {"GEMINI_API_KEY", "GOOGLE_API_KEY"},
	"kimi-cli":    {"MOONSHOT_API_KEY", "KIMI_API_KEY"},
}

// crossVendorStrip returns environment entries that blank every vendor API key
// not owned by kind, isolating a single-vendor agent from sibling credentials.
// It returns nil for a model-agnostic or unknown kind. Append the result AFTER
// the inherited environment so the blanks win.
func crossVendorStrip(kind string) []string {
	own, ok := vendorOwnKeys[kind]
	if !ok {
		return nil
	}
	var out []string
	for _, k := range allVendorAPIKeys {
		mine := false
		for _, o := range own {
			if o == k {
				mine = true
				break
			}
		}
		if !mine {
			out = append(out, k+"=")
		}
	}
	return out
}
