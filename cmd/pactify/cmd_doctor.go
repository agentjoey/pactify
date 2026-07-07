package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
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

func newDoctorCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "doctor", Short: "diagnose pactify install + repo wiring",
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, _ := os.Getwd()
			exe, _ := os.Executable()
			home, _ := os.UserHomeDir()
			checks := append(doctor.Run(cwd, paths.AgentID(), exe, os.Getenv("PATH"), home), checkMCP())

			allOK := true
			for _, ck := range checks {
				if !ck.OK {
					allOK = false
					break
				}
			}

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

			for _, ck := range checks {
				mark := "✓"
				if !ck.OK {
					mark = "✗"
				}
				fmt.Fprintf(c.OutOrStdout(), "%s %s — %s\n", mark, ck.Name, ck.Detail)
			}
			if !allOK {
				return fmt.Errorf("doctor found issues")
			}
			return nil
		}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit checks as a JSON array")
	return cmd
}
