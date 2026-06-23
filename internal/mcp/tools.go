package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/paths"
	"github.com/agentjoey/pactify/internal/registry"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type empty struct{}

// projectField is embedded in every action tool input: an optional registered
// project name. Empty = the session's launch cwd (the historical, cwd-bound
// behavior); set = address that project by name via the machine registry, so one
// MCP session (bound to one repo at launch) can drive any registered project
// (spec coordination-authority P2). The acting seat stays the session's
// PACT_AGENT_ID regardless of which project is addressed.
type projectField struct {
	Project string `json:"project,omitempty" jsonschema:"registered project name to act on (default: the session's launch directory)"`
}

type statusIn struct {
	projectField
}

// joinIn has no seat field: the seat is always the session's PACT_AGENT_ID,
// so a client cannot join as one seat while the log records another.
type joinIn struct {
	projectField
	Roles string `json:"roles,omitempty" jsonschema:"comma-separated roles"`
}
type assignIn struct {
	projectField
	Task     string   `json:"task"`
	Feature  string   `json:"feature"`
	Branch   string   `json:"branch"`
	Owner    string   `json:"owner"`
	Reviewer string   `json:"reviewer"`
	Spec     string   `json:"spec,omitempty"`
	Deps     []string `json:"deps,omitempty" jsonschema:"dep task ids in the same feature (must be accepted before owner joins)"`
}
type checkpointIn struct {
	projectField
	Task     string `json:"task"`
	Evidence string `json:"evidence"`
}
type taskIn struct {
	projectField
	Task string `json:"task"`
}
type changesIn struct {
	projectField
	Task   string `json:"task"`
	Reason string `json:"reason,omitempty"`
}
type featureIn struct {
	projectField
	Feature string `json:"feature"`
}

// resolveProject returns a dir-aware pact handle for the named project (looked up
// in the machine registry ~/.pactify/projects.json), or the cwd default when name
// is empty. This is what breaks the MCP server's launch-cwd binding: a session
// started in one repo can act on any registered project by name (spec
// coordination-authority P2). The acting seat is always PACT_AGENT_ID — addressing
// changes WHICH project, never WHO you are.
func resolveProject(name string) (*pact.Project, error) {
	if name == "" {
		return pact.At("."), nil
	}
	reg, err := registry.Load()
	if err != nil {
		return nil, fmt.Errorf("load project registry: %w", err)
	}
	for _, p := range reg.Projects {
		if p.Name == name {
			return pact.At(p.Path), nil
		}
	}
	return nil, fmt.Errorf("unknown project %q; registered: %s", name, strings.Join(projectNames(reg), ", "))
}

// projectNames lists the registered project names, sorted, for error/discovery.
func projectNames(reg registry.Registry) []string {
	names := make([]string, 0, len(reg.Projects))
	for _, p := range reg.Projects {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names
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
	sdk.AddTool(s, &sdk.Tool{Name: "projects", Description: "List the registered projects this session can address by name (their pact boards live in separate repos)"},
		func(_ context.Context, _ *sdk.CallToolRequest, _ empty) (*sdk.CallToolResult, any, error) {
			reg, err := registry.Load()
			if err != nil {
				return nil, nil, err
			}
			if len(reg.Projects) == 0 {
				return textResult("no projects registered"), nil, nil
			}
			var b strings.Builder
			for _, p := range reg.Projects {
				fmt.Fprintf(&b, "%s\t%s\n", p.Name, p.Path)
			}
			return textResult(strings.TrimRight(b.String(), "\n")), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "status", Description: "Print the project's pact STATE.yml (rendered from the log). Pass `project` to read any registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in statusIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return nil, nil, err
			}
			text, err := proj.Status()
			if err != nil {
				return nil, nil, err
			}
			return textResult(text), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "join", Description: "Worker cold-start: register this session's seat (PACT_AGENT_ID) and check out its feature branch. Pass `project` to join a registered project."},
		func(_ context.Context, req *sdk.CallToolRequest, in joinIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return nil, nil, err
			}
			seat := paths.AgentID()
			name, version := clientInfo(req)
			if err := proj.JoinWithClient(seat, in.Roles, name, version); err != nil {
				return nil, nil, err
			}
			return textResult(fmt.Sprintf("joined %s", seat)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "assign", Description: "Assign a task (owner must differ from reviewer; task ids unique). Pass `project` to target a registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in assignIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return nil, nil, err
			}
			if err := proj.Assign(in.Task, in.Feature, in.Branch, in.Owner, in.Reviewer, in.Spec, in.Deps); err != nil {
				return nil, nil, err
			}
			return textResult(fmt.Sprintf("assigned %s", in.Task)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "checkpoint", Description: "Submit a task for review with evidence (owner-only; commits the work). Pass `project` to target a registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in checkpointIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return nil, nil, err
			}
			if err := proj.Checkpoint(in.Task, in.Evidence); err != nil {
				return nil, nil, err
			}
			return textResult(fmt.Sprintf("checkpointed %s", in.Task)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "accept", Description: "Accept a task (reviewer-only; task must be awaiting_review). Pass `project` to target a registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in taskIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return nil, nil, err
			}
			if err := proj.Accept(in.Task); err != nil {
				return nil, nil, err
			}
			return textResult(fmt.Sprintf("accepted %s", in.Task)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "changes", Description: "Request changes on a task (reviewer-only; task must be awaiting_review). Pass `project` to target a registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in changesIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return nil, nil, err
			}
			if err := proj.Changes(in.Task, in.Reason); err != nil {
				return nil, nil, err
			}
			return textResult(fmt.Sprintf("changes requested on %s", in.Task)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "merge", Description: "Merge a feature branch into base (all tasks must be accepted). Pass `project` to target a registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in featureIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return nil, nil, err
			}
			if err := proj.Merge(in.Feature); err != nil {
				return nil, nil, err
			}
			return textResult(fmt.Sprintf("merged %s", in.Feature)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "validate", Description: "Run v1 conformance checks (drift/roster/rules/version). Pass `project` to target a registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in statusIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return nil, nil, err
			}
			if err := proj.Validate(); err != nil {
				return nil, nil, err
			}
			return textResult("valid"), nil, nil
		})
}
