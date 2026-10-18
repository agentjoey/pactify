package orchestrate

import (
	"os"
	"strings"

	"github.com/agentjoey/pactify/internal/vendorenv"
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

// crossVendorStrip keeps the orchestrator's call sites on the shared table.
func crossVendorStrip(kind string) []string { return vendorenv.Strip(kind) }

// allVendorAPIKeys is the shared vendor credential list (see vendorenv.APIKeys).
var allVendorAPIKeys = vendorenv.APIKeys
