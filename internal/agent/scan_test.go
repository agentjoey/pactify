package agent

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// byKind indexes scan results for per-kind assertions.
func byKind(rs []ScanResult) map[string]ScanResult {
	m := make(map[string]ScanResult, len(rs))
	for _, r := range rs {
		m[r.Kind] = r
	}
	return m
}

func TestScanWithCoversCLIAndDesktop(t *testing.T) {
	// claude and agy binaries resolve; claude-desktop config exists; everything
	// else misses. antigravity is a CLI kind now (detectBin "agy"), no longer
	// detected via its config path — agy-kind task, 2026-08-22.
	desktopPath := ExpandPath(registry["claude-desktop"].cfgPath)
	p := scanProbe{
		lookPath: func(bin string) (string, error) {
			if bin == "claude" || bin == "agy" {
				return "/usr/local/bin/" + bin, nil
			}
			return "", errors.New("not found")
		},
		statPath: func(path string) bool {
			return path == desktopPath
		},
	}

	got := byKind(scanWith(p))

	// CLI hit: detectBin resolves → installed with the binary path.
	if r := got["claude-code"]; !r.Installed || r.Detail != "/usr/local/bin/claude" {
		t.Errorf("claude-code: got %+v, want installed with binary path", r)
	}
	// antigravity CLI hit: detectBin "agy" resolves → installed with the binary path.
	if r := got["antigravity"]; !r.Installed || r.Detail != "/usr/local/bin/agy" {
		t.Errorf("antigravity: got %+v, want installed with binary path", r)
	}
	// CLI miss: detectBin does not resolve.
	if r := got["opencode"]; r.Installed || r.Detail != "not found" {
		t.Errorf("opencode: got %+v, want not installed / not found", r)
	}
	// Desktop hit: config path exists → installed with the config path.
	if r := got["claude-desktop"]; !r.Installed || r.Detail != desktopPath {
		t.Errorf("claude-desktop: got %+v, want installed with %q", r, desktopPath)
	}
	// Desktop miss: config path absent.
	if r := got["codex-app"]; r.Installed || r.Detail != "not found" {
		t.Errorf("codex-app: got %+v, want not installed / not found", r)
	}
}

func TestScanWithOrderMatchesKinds(t *testing.T) {
	p := scanProbe{
		lookPath: func(string) (string, error) { return "", errors.New("nope") },
		statPath: func(string) bool { return false },
	}
	rs := scanWith(p)

	order := make([]string, len(rs))
	for i, r := range rs {
		order[i] = r.Kind
	}
	if !reflect.DeepEqual(order, Kinds()) {
		t.Errorf("scan order = %v, want Kinds() order %v", order, Kinds())
	}
}

// Installed is the single-kind probe `agent register` uses to warn about a kind
// that is not on this machine. It must agree with Scan exactly — a second,
// slightly different detection rule is how "scan says installed, register warns
// anyway" bugs happen.
func TestInstalledWithMatchesScanPerKind(t *testing.T) {
	desktopPath := ExpandPath(registry["claude-desktop"].cfgPath)
	p := scanProbe{
		lookPath: func(bin string) (string, error) {
			if bin == "claude" {
				return "/usr/local/bin/claude", nil
			}
			return "", errors.New("not found")
		},
		statPath: func(path string) bool { return path == desktopPath },
	}

	scanned := byKind(scanWith(p))
	for _, kind := range Kinds() {
		got, ok := installedWith(p, kind)
		if !ok {
			t.Errorf("installedWith(%q): ok=false for a known kind", kind)
			continue
		}
		if got != scanned[kind] {
			t.Errorf("installedWith(%q) = %+v, want %+v (must match scan)", kind, got, scanned[kind])
		}
	}

	if got, ok := installedWith(p, "no-such-kind"); ok || got.Installed {
		t.Errorf("unknown kind: got %+v ok=%v, want not-ok and not-installed", got, ok)
	}
}

// The warning has to say WHAT was looked for: a bare "not installed" leaves the
// user guessing between a missing install and a PATH problem.
func TestNotInstalledHintNamesTheMissingThing(t *testing.T) {
	// CLI kind → the binary the probe looked for on PATH.
	if got := NotInstalledHint("opencode"); !strings.Contains(got, `"opencode"`) || !strings.Contains(got, "PATH") {
		t.Errorf("NotInstalledHint(opencode) = %q, want it to name the binary and PATH", got)
	}
	// Desktop kind → the config path that did not exist, expanded (no bare "~").
	got := NotInstalledHint("claude-desktop")
	want := ExpandPath(registry["claude-desktop"].cfgPath)
	if !strings.Contains(got, want) {
		t.Errorf("NotInstalledHint(claude-desktop) = %q, want it to contain %q", got, want)
	}
	if strings.Contains(got, "~") {
		t.Errorf("hint must show the expanded path, got %q", got)
	}
	if got := NotInstalledHint("no-such-kind"); got != "" {
		t.Errorf("unknown kind hint = %q, want empty", got)
	}
}

func TestScanDefaultProbeSmoke(t *testing.T) {
	// Must not panic with the real probe; we only assert one result per kind.
	rs := Scan()
	if len(rs) != len(Kinds()) {
		t.Errorf("Scan() returned %d results, want %d", len(rs), len(Kinds()))
	}
}
