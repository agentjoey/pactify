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
	return &cobra.Command{Use: "doctor [harness]", Short: "Check installed harnesses in disposable repositories (makes real API calls)", Args: func(c *cobra.Command, args []string) error {
		if err := cobra.MaximumNArgs(1)(c, args); err != nil {
			subExit(c, 2, err)
		}
		if len(args) == 1 {
			if _, ok := sub.Lookup(args[0]); !ok {
				subExit(c, 2, fmt.Errorf("未知 harness %q；可用：%s", args[0], strings.Join(sub.Names(), ", ")))
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
		passed, err := sub.Doctor(ctx, store, args, c.OutOrStdout())
		code := 0
		if !passed || err != nil {
			code = 1
		}
		subExit(c, code, err)
	}}
}
