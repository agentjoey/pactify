package serve

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
)

// wireCockpitDecisions reads the decision vocabulary the relay will accept on a
// cockpit.permission rpc (cloud/wire/src/rpc.ts CockpitPermissionRequest).
func wireCockpitDecisions(t *testing.T) map[string]bool {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("cannot locate test source path")
	}
	rpcTS := filepath.Join(filepath.Dir(thisFile), "..", "..", "cloud", "wire", "src", "rpc.ts")
	b, err := os.ReadFile(rpcTS)
	if err != nil {
		t.Skipf("wire schema not available (%v) — conformance guard skipped", err)
	}
	m := regexp.MustCompile(`(?s)export const CockpitPermissionRequest = z\.object\(\{.*?decision: z\.enum\(\[(.*?)\]\)`).FindSubmatch(b)
	if m == nil {
		t.Fatalf("could not find CockpitPermissionRequest's decision enum in %s", rpcTS)
	}
	out := map[string]bool{}
	for _, raw := range strings.Split(string(m[1]), ",") {
		if v := strings.Trim(strings.TrimSpace(raw), "'\""); v != "" {
			out[v] = true
		}
	}
	if len(out) == 0 {
		t.Fatalf("parsed an empty decision enum from %s", rpcTS)
	}
	return out
}

// Conformance guard for the approval-decision vocabulary, the counterpart of
// TestPactToWireKindConformsToWireEnum for AgentKind.
//
// Three surfaces spell this value and they do NOT agree by accident: Go says
// "allow_for_session" (cockpit.DecisionAllowForSession) while the wire rpc says
// "allow_session". Today that works only because cockpitDecision hand-bridges both
// spellings. Nothing enforced it — so a fourth decision added on one side (e.g.
// codex's `cancel`, backlog [CODEX-DECISION-CANCEL]) can be accepted locally and
// rejected at the relay, or vice versa, with no test to notice.
//
// The invariant is directional: every value the WIRE admits must be understood
// by the machine that receives it. A value the relay forwards but serve cannot
// parse is a dropped human decision.
func TestEveryWireDecisionIsUnderstoodByServe(t *testing.T) {
	for wireValue := range wireCockpitDecisions(t) {
		if _, ok := cockpitDecision(wireValue); !ok {
			t.Errorf("wire admits decision %q on cockpit.permission, but cockpitDecision rejects it — "+
				"a human's approval would be forwarded by the relay and then dropped here", wireValue)
		}
	}
}

// And the other direction: every decision this binary can act on must have a
// spelling the wire will carry, or a hosted operator can never send it.
func TestEveryServeDecisionIsExpressibleOnTheWire(t *testing.T) {
	admitted := wireCockpitDecisions(t)
	// The spellings serve accepts, mapped to the decision they produce.
	for _, spelling := range []string{"allow", "deny", "allow_for_session", "allow_session"} {
		if _, ok := cockpitDecision(spelling); !ok {
			continue // not an accepted spelling; nothing to require of the wire
		}
		if admitted[spelling] {
			return // at least one spelling of each decision travels
		}
	}
	t.Errorf("none of serve's accepted decision spellings appear in the wire enum %v", keysOf(admitted))
}

// Every Decision the cockpit backends can be handed must be producible from some
// spelling serve accepts — otherwise the constant is dead from the dashboard's
// point of view (which is currently true of nothing, and must stay that way).
func TestEveryCockpitDecisionIsReachableFromAnHTTPSpelling(t *testing.T) {
	reachable := map[cockpit.Decision]bool{}
	for _, spelling := range []string{"allow", "deny", "allow_for_session", "allow_session", "cancel"} {
		if d, ok := cockpitDecision(spelling); ok {
			reachable[d] = true
		}
	}
	for _, d := range []cockpit.Decision{
		cockpit.DecisionAllow,
		cockpit.DecisionDeny,
		cockpit.DecisionAllowForSession,
	} {
		if !reachable[d] {
			t.Errorf("cockpit.Decision %q cannot be produced from any spelling cockpitDecision accepts — "+
				"the backends implement it but no caller can ask for it", d)
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
