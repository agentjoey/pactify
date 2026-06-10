package main

import (
	"context"

	pactmcp "github.com/agentjoey/pactify/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{Use: "mcp", Short: "run the stdio MCP server (pact tools + resources for this project)",
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := context.Background()
			server := pactmcp.New()
			// A missing .pact dir is not fatal: tools fail closed per call.
			if stop, err := pactmcp.Watch(ctx, server); err == nil {
				defer stop()
			}
			return server.Run(ctx, &sdk.StdioTransport{})
		}}
}
