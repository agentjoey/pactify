package orchestrate

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// Security regression — review finding C2 (critical).
//
// The default cmd transport (osExec / osExecIdle) and the fix-until-green verify
// gate (shellExec) build the child environment from the full parent process env
// with no filtering. A spawned third-party agent binary (possibly prompt-injected)
// therefore inherits PACT_RELAY_TOKEN and PACTIFY_MASTER_SECRET and can exfiltrate
// them. The ACP and cockpit paths already strip these via filteredEnviron; the cmd
// path and the verify gate were never given the same treatment.
//
// These tests are RED on current main and turn GREEN once osExec and shellExec
// route the child env through a filtered (deny relay/pactify secrets) environment.
// The `env` subprocess prints exactly what it inherited, so the assertion is a
// direct observation of the leak.

const (
	relaySecretMarker  = "PACT_RELAY_TOKEN"
	masterSecretMarker = "PACTIFY_MASTER_SECRET"
)

// TestSEC_C2_CmdTransportDoesNotLeakRelaySecrets exercises the production execFn
// (osExec — what NewCmdRunner wires) with the two crown-jewel secrets set in the
// parent env, and asserts the spawned child does not receive them.
func TestSEC_C2_CmdTransportDoesNotLeakRelaySecrets(t *testing.T) {
	t.Setenv(relaySecretMarker, "relay-token-must-not-leak")
	t.Setenv(masterSecretMarker, "aabbccddeeff-master-must-not-leak")

	var out bytes.Buffer
	if err := osExec(context.Background(), "sh", []string{"-c", "env"}, t.TempDir(), nil, &out); err != nil {
		t.Fatalf("osExec(env): %v", err)
	}
	got := out.String()

	if strings.Contains(got, relaySecretMarker) {
		t.Errorf("C2: cmd transport leaked %s to the spawned agent's environment", relaySecretMarker)
	}
	if strings.Contains(got, masterSecretMarker) {
		t.Errorf("C2: cmd transport leaked %s to the spawned agent's environment", masterSecretMarker)
	}
	// The fix must FILTER, not clear: a normal variable (PATH) must still pass so
	// vendor auth and tool discovery keep working. Guards against over-stripping.
	if !strings.Contains(got, "PATH=") {
		t.Error("cmd transport dropped PATH — env passthrough broken (fix over-stripped)")
	}
}

// TestSEC_C2_VerifyGateDoesNotLeakRelaySecrets exercises shellExec — the gate that
// runs a planner-authored `verify:` command via `sh -c`. It currently sets no
// cmd.Env, inheriting the full parent env (same leak, compounded by the RCE surface
// of an AI-authored verify line). RED until shellExec sets a filtered cmd.Env.
func TestSEC_C2_VerifyGateDoesNotLeakRelaySecrets(t *testing.T) {
	t.Setenv(relaySecretMarker, "relay-token-must-not-leak")
	t.Setenv(masterSecretMarker, "aabbccddeeff-master-must-not-leak")

	// 4th arg is the per-run env map (a parallel session added it); nil exercises
	// the common gate path. The leak is independent of the map: shellExec bases
	// cmd.Env on the unfiltered os.Environ() either way.
	_, out, err := shellExec{}.Run(context.Background(), t.TempDir(), "env", nil)
	if err != nil {
		t.Fatalf("shellExec(env): %v", err)
	}
	if strings.Contains(out, relaySecretMarker) {
		t.Errorf("C2: verify gate leaked %s to the verify subprocess", relaySecretMarker)
	}
	if strings.Contains(out, masterSecretMarker) {
		t.Errorf("C2: verify gate leaked %s to the verify subprocess", masterSecretMarker)
	}
}
