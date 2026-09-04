package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/registry"
	"github.com/agentjoey/pactify/internal/runguard"
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

// joinIn has no seat field: the seat is the session's resolved identity
// (PACT_AGENT_ID, else the working copy's .pact/seat file — see ResolveSeat), so
// a client cannot join as one seat while the log records another.
type joinIn struct {
	projectField
	Roles string `json:"roles,omitempty" jsonschema:"comma-separated roles"`
	Task  string `json:"task,omitempty" jsonschema:"optional target task id: lift exactly this task (refused with a task-specific error when unknown, not owned by this seat, or dep-blocked) instead of the seat's first workable task"`
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
type acceptIn struct {
	projectField
	Task     string `json:"task"`
	Evidence string `json:"evidence,omitempty" jsonschema:"optional reviewer evidence backing the verdict (e.g. the verify command output summary); recorded on the accept event"`
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

// okResult returns a machine-readable success envelope: {"ok":true,"data":...}.
func okResult(data string) *sdk.CallToolResult {
	b, err := json.Marshal(map[string]any{"ok": true, "data": data})
	if err != nil {
		// Unreachable for this simple map with a valid string; fall back to an
		// error envelope so the caller never receives malformed content.
		return textResult(`{"ok":false,"error":"failed to marshal ok result"}`)
	}
	return textResult(string(b))
}

// errResult returns a machine-readable error envelope with IsError set. The
// returned Go error is nil so the SDK transports the failure detail inside the
// result content instead of dropping it at the protocol layer.
func errResult(err error) (*sdk.CallToolResult, any, error) {
	b, marshalErr := json.Marshal(map[string]any{"ok": false, "error": err.Error()})
	if marshalErr != nil {
		return nil, nil, marshalErr
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}, IsError: true}, nil, nil
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
				return errResult(err)
			}
			if len(reg.Projects) == 0 {
				return okResult("no projects registered"), nil, nil
			}
			var b strings.Builder
			for _, p := range reg.Projects {
				fmt.Fprintf(&b, "%s\t%s\n", p.Name, p.Path)
			}
			return okResult(strings.TrimRight(b.String(), "\n")), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "status", Description: "Print the project's pact STATE.yml (rendered from the log). Pass `project` to read any registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in statusIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return errResult(err)
			}
			text, err := proj.Status()
			if err != nil {
				return errResult(err)
			}
			return okResult(text), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "join", Description: "Worker cold-start: register this session's seat (resolved from PACT_AGENT_ID or the working copy's .pact/seat file) and check out its feature branch. Pass `task` to target a specific task (lifts exactly that task). Pass `project` to join a registered project."},
		func(_ context.Context, req *sdk.CallToolRequest, in joinIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return errResult(err)
			}
			// Resolve through the seat-identity chain rooted at the target project
			// (env > .pact/seat file), not env-only — so an MCP session whose host
			// didn't pass PACT_AGENT_ID still gets its working-copy seat.
			seat, _, _ := proj.ResolveSeat()
			name, version := clientInfo(req)
			if err := proj.JoinWithClientTask(seat, in.Roles, name, version, in.Task); err != nil {
				return errResult(err)
			}
			return okResult(fmt.Sprintf("joined %s", seat)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "assign", Description: "Assign a task (owner must differ from reviewer; task ids unique). Pass `project` to target a registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in assignIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return errResult(err)
			}
			if err := proj.Assign(in.Task, in.Feature, in.Branch, in.Owner, in.Reviewer, in.Spec, in.Deps); err != nil {
				return errResult(err)
			}
			return okResult(fmt.Sprintf("assigned %s", in.Task)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "checkpoint", Description: "Submit a task for review with evidence (owner-only; commits the work). Pass `project` to target a registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in checkpointIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return errResult(err)
			}
			// Checkpoint commits the whole worktree: taken while a driver is
			// mid-stint on another task, it sweeps that task's half-written files
			// into this one. The run's own task is exempt — this tool is exactly
			// how a briefed worker hands off.
			if blocked := runguard.CheckpointBlocked(proj.Dir(), in.Task); blocked != "" {
				return errResult(fmt.Errorf("%s\nwait for that run to finish; if the tree really is yours, a human can override with `pactify checkpoint %s --force`", blocked, in.Task))
			}
			if err := proj.Checkpoint(in.Task, in.Evidence); err != nil {
				return errResult(err)
			}
			return okResult(fmt.Sprintf("checkpointed %s", in.Task)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "accept", Description: "Accept a task (reviewer-only; task must be awaiting_review). Optionally pass `evidence` (your verify run / inspection summary) to record it on the accept event. Pass `project` to target a registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in acceptIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return errResult(err)
			}
			if err := proj.AcceptEvidence(in.Task, in.Evidence); err != nil {
				return errResult(err)
			}
			return okResult(fmt.Sprintf("accepted %s", in.Task)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "changes", Description: "Request changes on a task (reviewer-only; task must be awaiting_review). Pass `project` to target a registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in changesIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return errResult(err)
			}
			if err := proj.Changes(in.Task, in.Reason); err != nil {
				return errResult(err)
			}
			return okResult(fmt.Sprintf("changes requested on %s", in.Task)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "merge", Description: "Merge a feature branch into base (all tasks must be accepted). Pass `project` to target a registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in featureIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return errResult(err)
			}
			if err := proj.Merge(in.Feature); err != nil {
				return errResult(err)
			}
			return okResult(fmt.Sprintf("merged %s", in.Feature)), nil, nil
		})

	sdk.AddTool(s, &sdk.Tool{Name: "validate", Description: "Run v1 conformance checks (drift/roster/rules/version). Pass `project` to target a registered project."},
		func(_ context.Context, _ *sdk.CallToolRequest, in statusIn) (*sdk.CallToolResult, any, error) {
			proj, err := resolveProject(in.Project)
			if err != nil {
				return errResult(err)
			}
			if err := proj.Validate(); err != nil {
				return errResult(err)
			}
			return okResult("valid"), nil, nil
		})
}
