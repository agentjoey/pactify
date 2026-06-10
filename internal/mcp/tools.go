package mcp

import (
	"context"

	"github.com/agentjoey/pactify/internal/pact"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type empty struct{}

// textResult wraps plain text in a tool result.
func textResult(text string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}
}

func registerTools(s *sdk.Server) {
	sdk.AddTool(s, &sdk.Tool{Name: "status", Description: "Print the project's pact STATE.yml (rendered from the log)"},
		func(_ context.Context, _ *sdk.CallToolRequest, _ empty) (*sdk.CallToolResult, any, error) {
			text, err := pact.Status()
			if err != nil {
				return nil, nil, err
			}
			return textResult(text), nil, nil
		})
}
