package agent

import (
	"fmt"
	"os"
	"os/exec"
)

// ScanResult reports whether a single agent kind appears installed on this machine.
type ScanResult struct {
	Kind      string `json:"kind"`
	Installed bool   `json:"installed"`
	Detail    string `json:"detail"` // hit binary path / config path / "not found"
}

// scanProbe injects the OS-level checks so scanWith is testable without a real
// install: lookPath resolves a CLI binary, statPath reports a file's existence.
type scanProbe struct {
	lookPath func(string) (string, error) // production = exec.LookPath
	statPath func(string) bool            // production = os.Stat existence (after ExpandPath)
}

// osProbe is the production probe: PATH lookup for CLI kinds, file existence for
// desktop kinds.
func osProbe() scanProbe {
	return scanProbe{
		lookPath: exec.LookPath,
		statPath: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
	}
}

// Scan detects installed agent kinds using the real OS probes.
func Scan() []ScanResult { return scanWith(osProbe()) }

// Installed probes a SINGLE kind with exactly the checks `pactify agent scan`
// runs, so callers that only care about one kind (agent register) do not have to
// re-implement — or drift from — the detection rule. ok is false for an unknown
// kind.
func Installed(kind string) (r ScanResult, ok bool) { return installedWith(osProbe(), kind) }

func installedWith(p scanProbe, kind string) (ScanResult, bool) {
	s, ok := registry[kind]
	if !ok {
		return ScanResult{Kind: kind, Detail: "not found"}, false
	}
	return scanKind(p, kind, s), true
}

// NotInstalledHint explains, in one clause, WHAT the probe looked for and did not
// find. A bare "not installed" leaves the user guessing whether the binary is
// missing, misnamed, or merely off PATH — this names the thing to go fix. Empty
// for an unknown kind.
func NotInstalledHint(kind string) string {
	s, ok := registry[kind]
	if !ok {
		return ""
	}
	if s.detectBin != "" {
		return fmt.Sprintf("no %q binary on PATH", s.detectBin)
	}
	return fmt.Sprintf("no config found at %s", ExpandPath(s.cfgPath))
}

// scanWith is the testable core. Results follow Kinds() order.
func scanWith(p scanProbe) []ScanResult {
	out := make([]ScanResult, 0, len(registry))
	for _, kind := range Kinds() {
		out = append(out, scanKind(p, kind, registry[kind]))
	}
	return out
}

// scanKind resolves one kind: CLI kinds (detectBin set) are installed when the
// binary resolves on PATH; desktop kinds detect via their global Config().Path.
func scanKind(p scanProbe, kind string, s spec) ScanResult {
	r := ScanResult{Kind: kind, Detail: "not found"}
	if s.detectBin != "" {
		if path, err := p.lookPath(s.detectBin); err == nil {
			r.Installed = true
			r.Detail = path
		}
		return r
	}
	path := ExpandPath(s.cfgPath)
	if p.statPath(path) {
		r.Installed = true
		r.Detail = path
	}
	return r
}
