package serve

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/doctor"
)

// versionProbeTimeout bounds a single `<cli> --version`. Measured on this
// machine: claude 8ms, codex 138ms, gemini 572ms, kimi 402ms, opencode 207ms,
// agy 304ms. 5s is far above the slowest yet still bounds a hung binary — a
// settings panel must not wait on one wedged CLI.
const versionProbeTimeout = 5 * time.Second

// semver matches the first dotted version-looking token, with an optional `v`
// prefix. Three real output shapes were measured — "2.1.259 (Claude Code)",
// "codex-cli 0.144.4", and a bare "0.56.0" — and one extractor covers all three
// without a per-vendor table that would rot as CLIs change their banners.
//
// The leading `[^\w.]` guard is load-bearing: `\b\d` does NOT create a boundary
// inside "v2.3.4" (both are word characters), so a bare \b anchor silently
// matched "3.4" there. The capture group returns the digits without the `v`.
var semver = regexp.MustCompile(`[^\w.]v?(\d+\.\d+(?:\.\d+)?(?:[-.][0-9A-Za-z.]+)?)`)

// parseVersion pulls the version out of a `--version` line, or "" when the line
// carries none. Declining is deliberate: a wrong version in the collapsed row is
// worse than an empty one, because it silently misinforms.
func parseVersion(raw string) string {
	// Leading space so the guard class also covers a version at position 0.
	m := semver.FindStringSubmatch(" " + strings.TrimSpace(raw))
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// probeVersion runs `<command> --version` and extracts the version.
func probeVersion(ctx context.Context, command string) string {
	path, err := exec.LookPath(command)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	line := string(out)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return parseVersion(line)
}

// handleAgentVersions probes every drivable kind's CLI version IN PARALLEL.
//
// Sequential probing costs ~2s on a machine with eight installed CLIs (measured
// above), which is why this is its own endpoint rather than a field on
// GET /api/agents: the list must render immediately and fill versions in, not
// block on the slowest vendor binary.
func (s *Server) handleAgentVersions(w http.ResponseWriter, r *http.Request) {
	type result struct {
		kind, version string
	}
	var wg sync.WaitGroup
	ch := make(chan result, len(agent.Kinds()))
	for _, kind := range agent.Kinds() {
		spec, ok := agent.Get(kind)
		if !ok {
			continue
		}
		rs, ok := spec.Runner()
		if !ok {
			continue // GUI/desktop kind — nothing to ask for a version
		}
		wg.Add(1)
		go func(kind, command string) {
			defer wg.Done()
			if v := probeVersion(r.Context(), command); v != "" {
				ch <- result{kind, v}
			}
		}(kind, rs.Command)
	}
	wg.Wait()
	close(ch)

	versions := map[string]string{}
	for res := range ch {
		versions[res.kind] = res.version
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

// handleAgentTest runs the connectivity checks for ONE kind.
//
// It reuses doctor.VendorChecksFor rather than implementing its own probe: two
// rules for "is this agent usable" would eventually disagree, and a Test button
// that contradicts `pactify doctor` is worse than no button. The kind filter is
// on the check name, which doctor builds as "cli <kind>: <aspect>".
func (s *Server) handleAgentTest(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := agent.Get(kind); !ok {
		writeErr(w, http.StatusNotFound, "unknown agent kind "+kind)
		return
	}

	home, _ := os.UserHomeDir()
	prefix := "cli " + kind + ":"
	var checks []doctor.Check
	for _, c := range doctor.VendorChecksFor(home, os.Getenv("PATH"), map[string]bool{kind: true}) {
		if strings.HasPrefix(c.Name, prefix) {
			checks = append(checks, c)
		}
	}
	if len(checks) == 0 {
		// A kind with no headless runner (desktop app) has nothing to connect to.
		writeErr(w, http.StatusBadRequest, kind+" has no headless runner to test")
		return
	}

	ok := true
	for _, c := range checks {
		if !c.OK {
			ok = false
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "ok": ok, "checks": checks})
}
