package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func newSubDoctorCmd() *cobra.Command {
	var model string
	cmd := &cobra.Command{Use: "doctor [harness]", Short: "Check installed harnesses in disposable repositories (makes real API calls)", Args: func(c *cobra.Command, args []string) error {
		if err := cobra.MaximumNArgs(1)(c, args); err != nil {
			subExit(c, 2, err)
		}
		if c.Flags().Changed("model") && len(args) != 1 {
			subExit(c, 2, fmt.Errorf("--model requires exactly one harness"))
		}
		if len(args) == 1 {
			if _, ok := sub.Lookup(args[0]); !ok {
				subExit(c, 2, fmt.Errorf("unknown harness %q; available: %s", args[0], strings.Join(sub.Names(), ", ")))
			}
		}
		return nil
	}, Run: func(c *cobra.Command, args []string) {
		store, err := sub.DefaultStore()
		if err != nil {
			subExit(c, 1, err)
			return
		}
		ctx, stop := signal.NotifyContext(c.Context(), os.Interrupt)
		defer stop()
		passed, err := sub.Doctor(ctx, store, args, c.OutOrStdout(), sub.DoctorOptions{Model: model})
		code := 0
		if !passed || err != nil {
			code = 1
		}
		subExit(c, code, err)
	}}
	cmd.Flags().StringVar(&model, "model", "", "Requested probe model (requires one harness; must be available)")
	return cmd
}
