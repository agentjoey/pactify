package main

import (
	"fmt"
	"os"

	"github.com/agentjoey/pactify/internal/doctor"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{Use: "doctor", Short: "diagnose pactify install + repo wiring",
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, _ := os.Getwd()
			exe, _ := os.Executable()
			checks := doctor.Run(cwd, paths.AgentID(), exe, os.Getenv("PATH"))
			allOK := true
			for _, ck := range checks {
				mark := "✓"
				if !ck.OK {
					mark = "✗"
					allOK = false
				}
				fmt.Fprintf(c.OutOrStdout(), "%s %s — %s\n", mark, ck.Name, ck.Detail)
			}
			if !allOK {
				return fmt.Errorf("doctor found issues")
			}
			return nil
		}}
}
