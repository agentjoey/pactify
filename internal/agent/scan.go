package agent

import (
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

// Scan detects installed agent kinds using the real OS probes.
func Scan() []ScanResult {
	return scanWith(scanProbe{
		lookPath: exec.LookPath,
		statPath: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
	})
}

// scanWith is the testable core. CLI kinds (detectBin set) are installed when the
// binary resolves on PATH; desktop kinds detect via their global Config().Path.
// Results follow Kinds() order.
func scanWith(p scanProbe) []ScanResult {
	out := make([]ScanResult, 0, len(registry))
	for _, kind := range Kinds() {
		s := registry[kind]
		r := ScanResult{Kind: kind, Detail: "not found"}
		if s.detectBin != "" {
			if path, err := p.lookPath(s.detectBin); err == nil {
				r.Installed = true
				r.Detail = path
			}
		} else {
			path := ExpandPath(s.cfgPath)
			if p.statPath(path) {
				r.Installed = true
				r.Detail = path
			}
		}
		out = append(out, r)
	}
	return out
}
