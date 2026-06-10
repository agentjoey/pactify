package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// versionString renders the build identity line.
func versionString(version, commit, date string) string {
	return fmt.Sprintf("pactify %s (%s, %s)", version, commit, date)
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "print version, commit, and build date",
		RunE: func(c *cobra.Command, _ []string) error {
			fmt.Fprintln(c.OutOrStdout(), versionString(version, commit, date))
			return nil
		}}
}
