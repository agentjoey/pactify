package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/doctor"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/spf13/cobra"
)

// lingeringReader yields its payload, then holds the pipe open for delay before
// signalling EOF. The mcp server exits on stdin EOF, so an immediate EOF can race
// the flush of the initialize reply; the brief linger lets the handshake answer
// land (the mcp.bats smoke test relies on the same keep-stdin-open trick).
type lingeringReader struct {
	r     io.Reader
	delay time.Duration
	done  bool
}

func (lr *lingeringReader) Read(p []byte) (int, error) {
	n, err := lr.r.Read(p)
	if err == io.EOF && !lr.done {
		lr.done = true
		time.Sleep(lr.delay)
	}
	return n, err
}

// checkMCP spawns the PATH-resolved pactify (the binary agents will actually
// launch) and completes an MCP initialize handshake over stdio.
func checkMCP() doctor.Check {
	bin, err := exec.LookPath("pactify")
	if err != nil {
		return doctor.Check{Name: "mcp server", OK: false, Detail: "pactify not found on PATH — install.sh puts it there"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "mcp")
	cmd.Stdin = &lingeringReader{
		r: strings.NewReader(
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"doctor","version":"0"}}}` + "\n" +
				`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"),
		delay: 500 * time.Millisecond,
	}
	out, _ := cmd.Output() // EOF (after the linger) ends the server; we only need the handshake reply
	if !strings.Contains(string(out), `"pactify"`) {
		return doctor.Check{Name: "mcp server", OK: false, Detail: bin + " mcp did not answer the initialize handshake"}
	}
	return doctor.Check{Name: "mcp server", OK: true, Detail: bin + " mcp answers initialize"}
}

// gatingOK reports whether the exit code should be zero. An ADVISORY failure
// (a vendor CLI for an agent kind this project does not depend on — see
// doctor.RelevantKinds) is reported but never counted: doctor's exit code
// answers "is my setup OK for THIS project?", and a red for a toolchain the
// repo never touches is exactly the noise that teaches people to ignore it.
func gatingOK(checks []doctor.Check) bool {
	for _, ck := range checks {
		if !ck.OK && !ck.Advisory {
			return false
		}
	}
	return true
}

// renderChecks prints the human report. Three marks, so a reader never has to
// guess why a red line did not fail the command:
//
//	✓  passing
//	✗  failing — this is why doctor exits non-zero
//	!  failing, advisory — reported for completeness, does not affect the exit
//
// A legend is printed only when at least one advisory red is on screen.
func renderChecks(w io.Writer, checks []doctor.Check) {
	advisoryReds := 0
	for _, ck := range checks {
		mark := "✓"
		switch {
		case ck.OK:
		case ck.Advisory:
			mark = "!"
			advisoryReds++
		default:
			mark = "✗"
		}
		fmt.Fprintf(w, "%s %s — %s\n", mark, ck.Name, ck.Detail)
	}
	if advisoryReds > 0 {
		fmt.Fprintf(w, "\n! = %d advisory issue(s): an agent CLI this project does not use. "+
			"Reported so you can see the whole toolchain; not counted in the exit code.\n", advisoryReds)
	}
}

func newDoctorCmd() *cobra.Command {
	var asJSON bool
	var setupBridge bool
	cmd := &cobra.Command{Use: "doctor", Short: "diagnose pactify install + repo wiring",
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, _ := os.Getwd()

			if setupBridge {
				repoRoot, ok := doctor.FindRepoRoot(cwd)
				if !ok {
					exe, _ := os.Executable()
					repoRoot, ok = doctor.FindRepoRoot(filepath.Dir(exe))
				}
				if !ok {
					return fmt.Errorf("could not locate bridge/claude-host from cwd or exe")
				}
				if err := doctor.SetupBridge(repoRoot, c.OutOrStdout()); err != nil {
					return err
				}
				fmt.Fprintf(c.OutOrStdout(), "bridge ready at %s\n", repoRoot)
				return nil
			}

			exe, _ := os.Executable()
			home, _ := os.UserHomeDir()
			// Resolve the acting identity through the full chain (env > .pact/seat
			// file), not env-only, so doctor reports the same seat the verbs use.
			seatID, _ := paths.AgentIDIn(cwd)
			checks := append(doctor.Run(cwd, seatID, exe, os.Getenv("PATH"), home), checkMCP())

			if repoRoot, ok := doctor.FindRepoRoot(cwd); ok {
				checks = append(checks, doctor.BridgeChecks(repoRoot)...)
			}

			allOK := gatingOK(checks)

			if asJSON {
				enc := json.NewEncoder(c.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(checks); err != nil {
					return err
				}
				if !allOK {
					return fmt.Errorf("doctor found issues")
				}
				return nil
			}

			renderChecks(c.OutOrStdout(), checks)
			if !allOK {
				return fmt.Errorf("doctor found issues")
			}
			return nil
		}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit checks as a JSON array")
	cmd.Flags().BoolVar(&setupBridge, "setup-bridge", false, "materialize the claude cockpit bridge node deps (npm ci)")
	return cmd
}
