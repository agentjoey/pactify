// Package mcp exposes the pact protocol over the Model Context Protocol:
// every verb as a tool, STATE/log as resources, log changes as notifications.
package mcp

import (
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// New builds the pactify MCP server with all tools and resources registered.
func New() *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "pactify", Version: "v1"}, nil)
	registerTools(s)
	registerResources(s)
	return s
}
