package orchestrate

import (
	"context"
	"testing"
)

// Security regression — review finding M11 (cross-vendor credential leak).
//
// The child environment for an agent stint is built from the parent process env
// (minus pactify/relay secrets). Vendor API keys pass through, so a third-party
// agent — kimi (Moonshot), gemini (Google), codex (OpenAI) — inherited the user's
// real ANTHROPIC_API_KEY (and every other sibling vendor's key) and could
// exfiltrate it. A single-vendor agent must only see its OWN vendor's key.

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// crossVendorStrip blanks every vendor key not owned by the kind, and leaves a
// model-agnostic / unknown kind untouched.
func TestSEC_M11_CrossVendorStrip(t *testing.T) {
	cases := []struct {
		kind    string
		blanked []string // must be emitted as KEY= (blanked)
		kept    []string // must NOT be emitted (the kind's own key, inherited)
	}{
		{"kimi-cli", []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_APPLICATION_CREDENTIALS"}, []string{"MOONSHOT_API_KEY", "KIMI_API_KEY"}},
		{"claude-code", []string{"OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_APPLICATION_CREDENTIALS", "MOONSHOT_API_KEY", "KIMI_API_KEY"}, []string{"ANTHROPIC_API_KEY"}},
		{"codex-cli", []string{"ANTHROPIC_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_APPLICATION_CREDENTIALS", "MOONSHOT_API_KEY", "KIMI_API_KEY"}, []string{"OPENAI_API_KEY"}},
		// gemini-cli owns Application Default Credentials: GOOGLE_APPLICATION_CREDENTIALS
		// is a real alternate auth tier for it, so it must be INHERITED here (kept),
		// unlike for every other kind.
		{"gemini-cli", []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "MOONSHOT_API_KEY", "KIMI_API_KEY"}, []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_APPLICATION_CREDENTIALS"}},
		// antigravity (agy) authenticates via an OAuth token file, not any of
		// these env vars (verified 2026-08-22 — see internal/agent/launch.go and
		// internal/orchestrate/env.go comments), so it owns none of them: every
		// vendor key gets blanked, none are "kept". GOOGLE_APPLICATION_CREDENTIALS
		// included: `strings $(which agy)` carries it as an alternate-auth fallback,
		// so leaving it would let agy silently authenticate as whatever service
		// account happens to be configured on the machine instead of its OAuth file.
		{"antigravity", []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_APPLICATION_CREDENTIALS", "MOONSHOT_API_KEY", "KIMI_API_KEY"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			got := crossVendorStrip(tc.kind)
			for _, k := range tc.blanked {
				if !containsStr(got, k+"=") {
					t.Errorf("%s must blank sibling key %q; got %v", tc.kind, k, got)
				}
			}
			for _, k := range tc.kept {
				if containsStr(got, k+"=") {
					t.Errorf("%s must NOT blank its own key %q; got %v", tc.kind, k, got)
				}
			}
			// Every key in the table must be accounted for as either blanked or
			// kept, so adding a credential to allVendorAPIKeys without deciding who
			// owns it fails here instead of silently leaking to some kind.
			if n := len(tc.blanked) + len(tc.kept); n != len(allVendorAPIKeys) {
				t.Errorf("%s: table covers %d keys but allVendorAPIKeys has %d (%v) — "+
					"a newly added vendor credential must be classified for every kind",
					tc.kind, n, len(allVendorAPIKeys), allVendorAPIKeys)
			}
		})
	}

	// GOOGLE_APPLICATION_CREDENTIALS must be in the strip universe at all: it is a
	// Google service-account path, i.e. a live credential, and was originally
	// missing from allVendorAPIKeys (so no kind stripped it).
	if !containsStr(allVendorAPIKeys, "GOOGLE_APPLICATION_CREDENTIALS") {
		t.Error("GOOGLE_APPLICATION_CREDENTIALS must be listed in allVendorAPIKeys — " +
			"otherwise no single-vendor kind ever blanks it")
	}
	// antigravity owns NOTHING: its strip must cover the whole key universe, so a
	// future key added to allVendorAPIKeys is automatically denied to agy.
	if got := crossVendorStrip("antigravity"); len(got) != len(allVendorAPIKeys) {
		t.Errorf("antigravity strip = %v (%d entries), want all %d vendor keys blanked",
			got, len(got), len(allVendorAPIKeys))
	}
	// Model-agnostic (opencode) and unknown/custom kinds may legitimately use any
	// provider, so their environment is left untouched.
	for _, kind := range []string{"opencode", "custom-thing", ""} {
		if got := crossVendorStrip(kind); got != nil {
			t.Errorf("model-agnostic kind %q must not be stripped; got %v", kind, got)
		}
	}
}

// The ACP transport wires crossVendorStrip through acpEnv: a kimi stint blanks
// the sibling vendors' keys while leaving its own (MOONSHOT/KIMI) to be inherited.
func TestSEC_M11_AcpStripsSiblingVendorKeys(t *testing.T) {
	fc := newFakeAcpConn()
	var cap acpLaunch
	r := AcpRunner{Spawn: captureSpawn(fc, &cap)}
	if err := r.Run(context.Background(), LaunchContext{Seat: "w", Kind: "kimi-cli", RepoDir: "/tmp/x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, k := range []string{"ANTHROPIC_API_KEY=", "OPENAI_API_KEY=", "GEMINI_API_KEY="} {
		if !cap.hasEnv(k) {
			t.Errorf("kimi ACP must blank sibling key %q; env=%v", k, cap.env)
		}
	}
	for _, k := range []string{"MOONSHOT_API_KEY=", "KIMI_API_KEY="} {
		if cap.hasEnv(k) {
			t.Errorf("kimi ACP must NOT blank its own key %q; env=%v", k, cap.env)
		}
	}
}
