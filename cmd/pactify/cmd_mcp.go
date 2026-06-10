package main

import (
	"context"
	"os"
	"path/filepath"

	pactmcp "github.com/agentjoey/pactify/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// rootProject chdirs to dir (absolute) so both .pact resolution and git verbs
// anchor at that repo. Empty dir is a no-op (cwd behavior, used by CLI surfaces).
func rootProject(dir string) error {
	if dir == "" {
		return nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	return os.Chdir(abs)
}

func newMCPCmd() *cobra.Command {
	var project string
	c := &cobra.Command{Use: "mcp", Short: "run the stdio MCP server (pact tools + resources for this project)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rootProject(project); err != nil {
				return err
			}
			ctx := context.Background()
			server := pactmcp.New()
			if stop, err := pactmcp.Watch(ctx, server); err == nil {
				defer stop()
			}
			return server.Run(ctx, &sdk.StdioTransport{})
		}}
	c.Flags().StringVar(&project, "project", "", "repo to root the server at (for desktop apps whose cwd is not the repo)")
	return c
}
