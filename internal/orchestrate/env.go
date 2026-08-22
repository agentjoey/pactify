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
	// Application Default Credentials: a Google service-account path, and a real
	// alternate auth tier for both Google kinds (`strings $(which agy)` carries it
	// alongside GOOGLE_API_KEY/GEMINI_API_KEY). Listed here so a NON-Google kind
	// cannot inherit it, and so antigravity — which owns no key and must reach its
	// OAuth token file — cannot silently authenticate as whatever service account
	// happens to be configured on the machine.
	"GOOGLE_APPLICATION_CREDENTIALS",
}

// vendorOwnKeys maps a single-vendor agent kind to the API keys that ARE its own.
// A kind absent here (opencode, custom kinds) is model-agnostic — it may
// legitimately use any provider — so its environment is left untouched.
var vendorOwnKeys = map[string][]string{
	"claude-code": {"ANTHROPIC_API_KEY"},
	"codex-cli":   {"OPENAI_API_KEY"},
	"gemini-cli":  {"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_APPLICATION_CREDENTIALS"},
	"kimi-cli":    {"MOONSHOT_API_KEY", "KIMI_API_KEY"},
	// antigravity (agy) authenticates via an OAuth token FILE
	// (~/.gemini/antigravity-cli/antigravity-oauth-token) by default — but
	// `strings $(which agy)` (re-verified 2026-08-22 by independent review)
	// DOES contain GOOGLE_API_KEY/GEMINI_API_KEY/GOOGLE_APPLICATION_CREDENTIALS
	// as alternate-auth fallback strings (agy warns if both GOOGLE_API_KEY and
	// GEMINI_API_KEY are set). So agy CAN authenticate via those — but they are
	// gemini-cli's credential, not agy's own, and `env -u <all of the above>
	// agy -p ...` still authenticates fine via the OAuth file alone. Treating
	// them as antigravity's "own" keys would be wrong (that's what gemini-cli
	// owns); the safe choice is the opposite — explicit empty slice (present in
	// the map, owns nothing) so crossVendorStrip blanks every sibling vendor key,
	// including GEMINI_API_KEY/GOOGLE_API_KEY, from agy's environment. This also
	// prevents agy from silently falling back to a leaked/wrong-vendor key
	// instead of its intended OAuth file. An omitted map key (vs. an explicit
	// empty slice) is treated as model-agnostic and left UNSTRIPPED — see
	// crossVendorStrip — which is not what we want here.
	//
	// Verified 2026-08-22 under the mechanism actually used (blanking to the EMPTY
	// STRING, not unsetting — os/exec keeps the last duplicate, so a trailing
	// `KEY=` shadows the inherited value): `agy -p ... --output-format json` with
	// all six keys blanked still authenticates via the OAuth file and returns
	// SUCCESS. The earlier evidence for this used `env -u`, which is not the same
	// thing — a CLI testing for presence rather than non-emptiness would behave
	// differently between the two.
	"antigravity": {},
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
