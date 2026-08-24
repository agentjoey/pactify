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
	// antigravity (binary agy) authenticates via an OAuth token FILE. It is not
	// the ONLY path agy accepts — GOOGLE_API_KEY/GEMINI_API_KEY/
	// GOOGLE_APPLICATION_CREDENTIALS are alternate tiers baked into the binary
	// (see orchestrate/env.go, which strips all three) — but it is the only one
	// pactify permits: an orchestrated agy stint has every vendor key blanked, so
	// the OAuth file is load-bearing for exactly the runs doctor is preflighting.
	// Hence lenient=false: an absent token is a real red, same as codex-cli/
	// gemini-cli. Caveat this reports a false red for someone who drives agy by
	// hand on an API key and never through pactify.
	"antigravity": {authRel: ".gemini/antigravity-cli/antigravity-oauth-token",
		installHint: "install the Antigravity CLI (agy) — see antigravity.google docs",
		loginHint:   "run `agy` once and complete the OAuth login flow"},
}

// notUsedNote is appended to a failing check for a kind this project does not
// depend on, so a red line on screen never leaves the reader wondering why the
// command still exited zero.
const notUsedNote = "not used by this project, so this does not fail doctor"

// VendorChecks returns the per-vendor-CLI preflight checks with NO project
// scoping: every kind gates. That is the right contract for its caller,
// `pactify serve`, which preflights a whole machine hosting many projects and
// cannot know which vendor any one of them needs.
func VendorChecks(home, pathEnv string) []Check {
	return VendorChecksFor(home, pathEnv, nil)
}

// VendorChecksFor returns the per-vendor-CLI preflight checks: for every kind
// with a headless runner spec, a binary-on-PATH check, an optional static auth
// check, and a transport-availability report. PATH and HOME are injected (never
// read from the process env) so the whole set is hermetically testable.
//
// relevant scopes the EXIT CODE, never the report: kinds outside the set still
// produce their full set of checks, marked Advisory (and, when red, annotated
// with notUsedNote). A nil map means "no scoping" — everything gates; an EMPTY
// map means nothing gates, which is what a directory with no pact project
// yields. See RelevantKinds.
func VendorChecksFor(home, pathEnv string, relevant map[string]bool) []Check {
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

		var kindChecks []Check
		kindChecks = append(kindChecks, vendorBinaryCheck(kind, rs.Command, pathEnv, probe))
		if probe.authRel != "" || probe.authAltRel != "" {
			kindChecks = append(kindChecks, vendorAuthCheck(kind, home, probe))
		}
		kindChecks = append(kindChecks, vendorTransportCheck(kind))

		if relevant != nil && !relevant[kind] {
			for i := range kindChecks {
				kindChecks[i] = markAdvisory(kindChecks[i])
			}
		}
		checks = append(checks, kindChecks...)
	}
	return checks
}

// markAdvisory demotes a check to report-only. The note is appended only to
// FAILING checks: a green line needs no excuse, and repeating the note on every
// passing vendor would bury the two lines that matter.
func markAdvisory(c Check) Check {
	c.Advisory = true
	if !c.OK {
		c.Detail = c.Detail + " [" + notUsedNote + "]"
	}
	return c
}

func vendorBinaryCheck(kind, command, pathEnv string, probe authProbe) Check {
	name := fmt.Sprintf("cli %s: binary", kind)
	if path, ok := lookInPath(command, pathEnv); ok {
		return Check{Name: name, OK: true, Detail: path}
	}
	hint := probe.installHint
	if hint == "" {
		hint = "install the " + command + " CLI"
	}
	return Check{Name: name, OK: false, Detail: fmt.Sprintf("%q not on PATH — %s", command, hint)}
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
		return Check{Name: name, OK: true, Detail: "credentials present at " + found}
	}
	target := filepath.Join(home, probe.authRel)
	if probe.lenient {
		// Absent but not fatal — keep green, surface an advisory in Detail.
		return Check{Name: name, OK: true, Detail: fmt.Sprintf("no %s; %s", target, probe.loginHint)}
	}
	return Check{Name: name, OK: false, Detail: fmt.Sprintf("no %s — %s", target, probe.loginHint)}
}

func vendorTransportCheck(kind string) Check {
	name := fmt.Sprintf("cli %s: transport", kind)
	if acpKinds[kind] {
		return Check{Name: name, OK: true, Detail: "transport: acp available"}
	}
	return Check{Name: name, OK: true, Detail: "transport: cmd only"}
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
