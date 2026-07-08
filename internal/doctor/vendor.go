package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/agent"
)

// acpKinds are the kinds with a known ACP transport bridge (spec §4 / §1 A.2
// mapping table). Membership drives the "transport: acp available" report;
// everything else with a headless runner is "cmd only".
var acpKinds = map[string]bool{
	"kimi-cli":    true,
	"claude-code": true,
	"codex-cli":   true,
	"gemini-cli":  true,
	"opencode":    true,
}

// authProbe describes the static (no-network) credential check for a kind.
// authRel is the credential path relative to HOME; "" means the kind needs no
// auth check (opencode / local models). isDir treats the path as a directory
// (kimi's ~/.kimi). lenient means an absent credential is NOT a hard failure —
// the vendor may authenticate elsewhere (claude via macOS Keychain, kimi via a
// session file we don't model) — so the check stays green with an advisory note.
type authProbe struct {
	authRel     string
	authAltRel  string // alternate credential path; either authRel or authAltRel may satisfy auth
	isDir       bool
	lenient     bool
	installHint string
	loginHint   string
}

// vendorAuth is the per-kind preflight metadata. Kinds absent from this map that
// still have a headless runner (e.g. a custom registered kind) get a binary +
// transport check with auth skipped.
var vendorAuth = map[string]authProbe{
	"claude-code": {authRel: ".claude/.credentials.json", lenient: true,
		installHint: "install the Claude Code CLI (npm i -g @anthropic-ai/claude-code)",
		loginHint:   "run `claude login` (or it may authenticate via the macOS Keychain)"},
	"codex-cli": {authRel: ".codex/auth.json",
		installHint: "install codex (npm i -g @openai/codex)",
		loginHint:   "run `codex login`"},
	"gemini-cli": {authRel: ".gemini/oauth_creds.json", authAltRel: ".gemini/google_accounts.json",
		installHint: "install gemini-cli (npm i -g @google/gemini-cli)",
		loginHint:   "run `gemini` once to authenticate"},
	"kimi-cli": {authRel: ".kimi", isDir: true, lenient: true,
		installHint: "install the kimi CLI",
		loginHint:   "run `kimi login`"},
	"opencode": {installHint: "install opencode (see opencode.ai)"},
}

// VendorChecks returns the per-vendor-CLI preflight checks: for every kind with a
// headless runner spec, a binary-on-PATH check, an optional static auth check,
// and a transport-availability report. PATH and HOME are injected (never read
// from the process env) so the whole set is hermetically testable.
func VendorChecks(home, pathEnv string) []Check {
	var checks []Check
	for _, kind := range agent.Kinds() {
		spec, ok := agent.Get(kind)
		if !ok {
			continue
		}
		rs, ok := spec.Runner()
		if !ok {
			continue // no headless runner — GUI/desktop or unverified CLI
		}
		probe := vendorAuth[kind] // zero value is fine for unknown custom kinds

		checks = append(checks, vendorBinaryCheck(kind, rs.Command, pathEnv, probe))
		if probe.authRel != "" || probe.authAltRel != "" {
			checks = append(checks, vendorAuthCheck(kind, home, probe))
		}
		checks = append(checks, vendorTransportCheck(kind))
	}
	return checks
}

func vendorBinaryCheck(kind, command, pathEnv string, probe authProbe) Check {
	name := fmt.Sprintf("cli %s: binary", kind)
	if path, ok := lookInPath(command, pathEnv); ok {
		return Check{name, true, path}
	}
	hint := probe.installHint
	if hint == "" {
		hint = "install the " + command + " CLI"
	}
	return Check{name, false, fmt.Sprintf("%q not on PATH — %s", command, hint)}
}

func vendorAuthCheck(kind, home string, probe authProbe) Check {
	name := fmt.Sprintf("cli %s: auth", kind)

	var candidates []string
	if probe.authRel != "" {
		candidates = append(candidates, probe.authRel)
	}
	if probe.authAltRel != "" {
		candidates = append(candidates, probe.authAltRel)
	}

	var found string
	for _, rel := range candidates {
		target := filepath.Join(home, rel)
		present := probe.isDir && isDir(target) || !probe.isDir && isNonEmptyFile(target)
		if present {
			found = target
			break
		}
	}

	if found != "" {
		return Check{name, true, "credentials present at " + found}
	}
	target := filepath.Join(home, probe.authRel)
	if probe.lenient {
		// Absent but not fatal — keep green, surface an advisory in Detail.
		return Check{name, true, fmt.Sprintf("no %s; %s", target, probe.loginHint)}
	}
	return Check{name, false, fmt.Sprintf("no %s — %s", target, probe.loginHint)}
}

func vendorTransportCheck(kind string) Check {
	name := fmt.Sprintf("cli %s: transport", kind)
	if acpKinds[kind] {
		return Check{name, true, "transport: acp available"}
	}
	return Check{name, true, "transport: cmd only"}
}

// lookInPath resolves command against the injected pathEnv, mirroring
// exec.LookPath semantics (executable-bit check) but without reading the process
// PATH — so callers/tests inject a fake PATH. A command containing a path
// separator is checked directly.
func lookInPath(command, pathEnv string) (string, bool) {
	if command == "" {
		return "", false
	}
	if strings.ContainsRune(command, os.PathSeparator) {
		if isExecutable(command) {
			return command, true
		}
		return "", false
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			dir = "."
		}
		p := filepath.Join(dir, command)
		if isExecutable(p) {
			return p, true
		}
	}
	return "", false
}

func isExecutable(p string) bool {
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func isNonEmptyFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Size() > 0
}
