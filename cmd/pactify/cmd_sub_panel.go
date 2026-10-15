package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/agentjoey/pactify/internal/subpanel"
	"github.com/spf13/cobra"
)

func newSubPanelCmd() *cobra.Command {
	var port int
	var noOpen bool
	command := &cobra.Command{Use: "panel", Short: "Open the read-only local job panel", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		store, err := sub.DefaultStore()
		if err != nil {
			return err
		}
		panel, err := subpanel.New(store)
		if err != nil {
			return err
		}
		ln, err := subpanel.Listen(port)
		if err != nil {
			return err
		}
		defer ln.Close()
		address := panel.URL(ln)
		if _, err := fmt.Fprintln(c.OutOrStdout(), address); err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(c.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if !noOpen {
			go func() {
				openCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				if err := openSubPanel(openCtx, address); err != nil && ctx.Err() == nil {
					fmt.Fprintf(c.ErrOrStderr(), "Browser could not open; use the URL above: %v\n", err)
				}
			}()
		}
		return panel.Serve(ctx, ln)
	}}
	command.Flags().IntVar(&port, "port", 0, "Loopback port (0 selects a random port)")
	command.Flags().BoolVar(&noOpen, "no-open", false, "Print the URL without opening a browser")
	return command
}
func openSubPanel(ctx context.Context, address string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.CommandContext(ctx, "open", address)
	case "windows":
		command = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", address)
	default:
		command = exec.CommandContext(ctx, "xdg-open", address)
	}
	return command.Run()
}
