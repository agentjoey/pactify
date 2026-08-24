package serve

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// antigravity (the `agy` CLI) became a first-class headless kind on 2026-08-22:
// it has a RunnerProfile, is drivable, and is staffed as a real worker/reviewer.
// It must therefore be advertised to the relay like every other drivable kind —
// a missing mapping made machineAgentKinds silently drop it, so the hosted side
// could never see or route to an agy-capable machine.
func TestPactToWireKindMapsAntigravity(t *testing.T) {
	got, ok := pactToWireKind["antigravity"]
	if !ok {
		t.Fatalf("pactToWireKind is missing %q; machine would never advertise agy", "antigravity")
	}
	if got != "antigravity" {
		t.Fatalf("pactToWireKind[antigravity] = %q, want %q", got, "antigravity")
	}
}

// machineAgentKinds is the roster actually put on the wire. Every drivable kind
// pactify knows must survive the pact→wire translation.
func TestMachineAgentKindsAdvertisesAntigravity(t *testing.T) {
	s := &Server{}
	kinds := s.machineAgentKinds()
	if !contains(kinds, "antigravity") {
		t.Fatalf("machineAgentKinds() = %v, want it to include %q", kinds, "antigravity")
	}
	// Sanity: the pre-existing roster is untouched.
	for _, want := range []string{"claude", "codex", "gemini", "kimi", "opencode"} {
		if !contains(kinds, want) {
			t.Errorf("machineAgentKinds() = %v, missing pre-existing kind %q", kinds, want)
		}
	}
}

// Conformance guard: every value pactToWireKind puts on the wire MUST exist in
// the relay's zod AgentKind enum (cloud/wire/src/rpc.ts). The relay validates
// MachineInfo.agentKinds with a THROWING `MachineInfo.parse` (relay/src/machines.ts
// toMachineInfo), so a value outside the enum doesn't degrade gracefully — it
// throws inside listMachines and takes down the whole account's machine list /
// GET /v1/machines. Same class of bug as the `eventKind: "error"` value that
// exceeded the wire enum and got silently dropped; this test keeps the two
// vocabularies in lockstep.
func TestPactToWireKindConformsToWireEnum(t *testing.T) {
	allowed := wireAgentKinds(t)
	for pactKind, wireKind := range pactToWireKind {
		if !allowed[wireKind] {
			t.Errorf("pactToWireKind[%q] = %q, which is NOT in the wire AgentKind enum %v — the relay would reject/throw on register",
				pactKind, wireKind, keys(allowed))
		}
	}
}

// wireAgentKinds parses the AgentKind zod enum out of the TypeScript wire schema.
func wireAgentKinds(t *testing.T) map[string]bool {
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
	m := regexp.MustCompile(`(?s)export const AgentKind = z\.enum\(\[(.*?)\]\)`).FindSubmatch(b)
	if m == nil {
		t.Fatalf("could not find `export const AgentKind = z.enum([...])` in %s", rpcTS)
	}
	out := map[string]bool{}
	for _, raw := range strings.Split(string(m[1]), ",") {
		v := strings.Trim(strings.TrimSpace(raw), "'\"")
		if v != "" {
			out[v] = true
		}
	}
	if len(out) == 0 {
		t.Fatalf("parsed an empty AgentKind enum from %s", rpcTS)
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
