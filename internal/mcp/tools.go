package mcp

import (
	"context"
	"fmt"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type empty struct{}

// joinIn has no seat field: the seat is always the session's PACT_AGENT_ID,
// so a client cannot join as one seat while the log records another.
type joinIn struct {
	Roles string `json:"roles,omitempty" jsonschema:"comma-separated roles"`
}
type assignIn struct {
	Task     string   `json:"task"`
	Feature  string   `json:"feature"`
	Branch   string   `json:"branch"`
	Owner    string   `json:"owner"`
	Reviewer string   `json:"reviewer"`
	Spec     string   `json:"spec,omitempty"`
	Deps     []string `json:"deps,omitempty" jsonschema:"dep task ids in the same feature (must be accepted before owner joins)"`
}
type checkpointIn struct {
	Task     string `json:"task"`
	Evidence string `json:"evidence"`
}
type taskIn struct {
	Task string `json:"task"`
}
type changesIn struct {
	Task   string `json:"task"`
	Reason string `json:"reason,omitempty"`
}
type featureIn struct {
	Feature string `json:"feature"`
}

// textResult wraps plain text in a tool result.
func textResult(text string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}
}

// clientInfo extracts the connecting client's self-reported name/version from
// the session's initialize params. Nil-safe at every hop (no session, not yet
// initialized, or no clientInfo) → empty strings → JoinWithClient emits no
// client field. This is advisory provenance only, never an identity proof.
func clientInfo(req *sdk.CallToolRequest) (name, version string) {
	if req == nil || req.Session == nil {
		return "", ""
	}
	params := req.Session.InitializeParams()
	if params == nil || params.ClientInfo == nil {
		return "", ""
	}
	return params.ClientInfo.Name, params.ClientInfo.Version
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

	sdk.AddTool(s, &sdk.Tool{Name: "join", Description: "Worker cold-start: register this session's seat (PACT_AGENT_ID) and check out its feature branch"},
		func(_ context.Context, req *sdk.CallToolRequest, in joinIn) (*sdk.CallToolResult, any, error) {
			seat := paths.AgentID()
			name, version := clientInfo(req)
			if err := pact.JoinWithClient(seat, in.Roles, name, version); err != nil {
				return nil, nil, err
			}
			return textResult(fmt.Sprintf("joined %s", seat)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "assign", Description: "Assign a task (owner must differ from reviewer; task ids unique)"},
		func(_ context.Context, _ *sdk.CallToolRequest, in assignIn) (*sdk.CallToolResult, any, error) {
			if err := pact.Assign(in.Task, in.Feature, in.Branch, in.Owner, in.Reviewer, in.Spec, in.Deps); err != nil {
				return nil, nil, err
			}
			return textResult(fmt.Sprintf("assigned %s", in.Task)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "checkpoint", Description: "Submit a task for review with evidence (owner-only; commits the work)"},
		func(_ context.Context, _ *sdk.CallToolRequest, in checkpointIn) (*sdk.CallToolResult, any, error) {
			if err := pact.Checkpoint(in.Task, in.Evidence); err != nil {
				return nil, nil, err
			}
			return textResult(fmt.Sprintf("checkpointed %s", in.Task)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "accept", Description: "Accept a task (reviewer-only; task must be awaiting_review)"},
		func(_ context.Context, _ *sdk.CallToolRequest, in taskIn) (*sdk.CallToolResult, any, error) {
			if err := pact.Accept(in.Task); err != nil {
				return nil, nil, err
			}
			return textResult(fmt.Sprintf("accepted %s", in.Task)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "changes", Description: "Request changes on a task (reviewer-only; task must be awaiting_review)"},
		func(_ context.Context, _ *sdk.CallToolRequest, in changesIn) (*sdk.CallToolResult, any, error) {
			if err := pact.Changes(in.Task, in.Reason); err != nil {
				return nil, nil, err
			}
			return textResult(fmt.Sprintf("changes requested on %s", in.Task)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "merge", Description: "Merge a feature branch into base (all tasks must be accepted)"},
		func(_ context.Context, _ *sdk.CallToolRequest, in featureIn) (*sdk.CallToolResult, any, error) {
			if err := pact.Merge(in.Feature); err != nil {
				return nil, nil, err
			}
			return textResult(fmt.Sprintf("merged %s", in.Feature)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "validate", Description: "Run v1 conformance checks (drift/roster/rules/version)"},
		func(_ context.Context, _ *sdk.CallToolRequest, _ empty) (*sdk.CallToolResult, any, error) {
			if err := pact.Validate(); err != nil {
				return nil, nil, err
			}
			return textResult("valid"), nil, nil
		})
}
