package orchestrate

import (
	"context"
	"testing"
)

// Security regression — review finding M11 (cross-vendor credential leak).
//
// The child environment for an agent stint is built from the parent process env
// (minus pactify/relay secrets). Vendor API keys pass through, so a third-party
// agent — kimi (Moonshot), agy (Google), codex (OpenAI) — inherited the user's
// real ANTHROPIC_API_KEY (and every other sibling vendor's key) and could
// exfiltrate it. A single-vendor agent must only see its OWN vendor's key.

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
