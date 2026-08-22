package agent

import (
	"errors"
	"reflect"
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

func TestScanDefaultProbeSmoke(t *testing.T) {
	// Must not panic with the real probe; we only assert one result per kind.
	rs := Scan()
	if len(rs) != len(Kinds()) {
		t.Errorf("Scan() returned %d results, want %d", len(rs), len(Kinds()))
	}
}
