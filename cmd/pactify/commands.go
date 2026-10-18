package main

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "pactify", Short: "delegate tasks to coding agent harnesses", SilenceUsage: true, SilenceErrors: true}
	root.Version = versionString(version, commit, date)
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(newServeCmd(), newSubCmd(), newVersionCmd())
	return root
}
