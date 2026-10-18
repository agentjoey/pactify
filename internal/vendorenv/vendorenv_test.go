package vendorenv

import "testing"

// Security regression — review finding M11 (cross-vendor credential leak).
// A single-vendor agent must only see its OWN vendor's key; a model-agnostic
// kind is left untouched.

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// Strip blanks every vendor key not owned by the kind, and leaves a
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
		// antigravity (agy) authenticates via an OAuth token file, not any of
		// these env vars (verified 2026-08-22), so it owns none of them: every
		// vendor key gets blanked, none are "kept". GOOGLE_APPLICATION_CREDENTIALS
		// included: `strings $(which agy)` carries it as an alternate-auth fallback,
		// so leaving it would let agy silently authenticate as whatever service
		// account happens to be configured on the machine instead of its OAuth file.
		{"antigravity", []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_APPLICATION_CREDENTIALS", "MOONSHOT_API_KEY", "KIMI_API_KEY"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			got := Strip(tc.kind)
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
			// kept, so adding a credential to APIKeys without deciding who
			// owns it fails here instead of silently leaking to some kind.
			if n := len(tc.blanked) + len(tc.kept); n != len(APIKeys) {
				t.Errorf("%s: table covers %d keys but APIKeys has %d (%v) — "+
					"a newly added vendor credential must be classified for every kind",
					tc.kind, n, len(APIKeys), APIKeys)
			}
		})
	}

	// GOOGLE_APPLICATION_CREDENTIALS must be in the strip universe at all: it is a
	// Google service-account path, i.e. a live credential, and was originally
	// missing from APIKeys (so no kind stripped it).
	if !containsStr(APIKeys, "GOOGLE_APPLICATION_CREDENTIALS") {
		t.Error("GOOGLE_APPLICATION_CREDENTIALS must be listed in APIKeys — " +
			"otherwise no single-vendor kind ever blanks it")
	}
	// antigravity owns NOTHING: its strip must cover the whole key universe, so a
	// future key added to APIKeys is automatically denied to agy.
	if got := Strip("antigravity"); len(got) != len(APIKeys) {
		t.Errorf("antigravity strip = %v (%d entries), want all %d vendor keys blanked",
			got, len(got), len(APIKeys))
	}
	// Model-agnostic (opencode) and unknown/custom kinds may legitimately use any
	// provider, so their environment is left untouched.
	for _, kind := range []string{"opencode", "custom-thing", ""} {
		if got := Strip(kind); got != nil {
			t.Errorf("model-agnostic kind %q must not be stripped; got %v", kind, got)
		}
	}
}
