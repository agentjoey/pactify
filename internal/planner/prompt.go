package planner

import (
	"fmt"
	"strings"
)

// SeatInfo describes one roster seat available to the planner. Drivable is true
// when agent.Get(kind).Runner() succeeds (a headless agent we can launch); GUI
// seats are Drivable=false and require a human hand-off.
type SeatInfo struct {
	ID       string
	Roles    []string
	Drivable bool
}

// PromptInput is everything the caller gathers to scaffold the planning prompt.
type PromptInput struct {
	Goal     string     // what the user wants delivered
	Feature  string     // feature id / branch slug the plan targets
	RepoTree string     // top-level + key directory tree text (caller-collected)
	Seats    []SeatInfo // roster the plan may assign owners/reviewers from
}

// BuildPrompt assembles goal + repo structure + roster + pactify conventions +
// manifest schema into the instruction text handed to a planner agent. It is a
// pure, deterministic function of its input.
func BuildPrompt(in PromptInput) string {
	var b strings.Builder

	b.WriteString("You are the pactify PLANNER. Decompose a goal into the smallest set of\n")
	b.WriteString("independently deliverable pact tasks and emit (1) one spec file per task and\n")
	b.WriteString("(2) one machine-readable plan manifest. Follow these instructions exactly.\n\n")

	b.WriteString("## Goal\n")
	b.WriteString(in.Goal)
	b.WriteString("\n\n")

	b.WriteString("## Feature\n")
	b.WriteString(in.Feature)
	b.WriteString("\n\n")

	b.WriteString("## Repo structure\n")
	b.WriteString("```\n")
	b.WriteString(strings.TrimRight(in.RepoTree, "\n"))
	b.WriteString("\n```\n\n")

	b.WriteString("## Roster (assign owners and reviewers only from these seats)\n")
	for _, s := range in.Seats {
		drivable := "Drivable=false (GUI — requires human hand-off, use only when unavoidable)"
		if s.Drivable {
			drivable = "Drivable=true (headless — can be launched automatically)"
		}
		fmt.Fprintf(&b, "- %s · roles: [%s] · %s\n", s.ID, strings.Join(s.Roles, ", "), drivable)
	}
	b.WriteString("\n")

	b.WriteString("## How to decompose\n")
	b.WriteString("- Break the goal into the SMALLEST set of independently deliverable tasks.\n")
	b.WriteString("- Order tasks as a dependency chain. The chain must be SERIAL: only one task\n")
	b.WriteString("  may run at a time, because all agents share a single git worktree —\n")
	b.WriteString("  concurrent owners would collide. Encode order via each task's `deps`.\n")
	b.WriteString("- Each task must be small, verifiable, and touch a bounded set of files.\n\n")

	b.WriteString("## Seat assignment rules\n")
	b.WriteString("- owner ≠ reviewer for every task (a worker cannot accept its own work; they\n")
	b.WriteString("  must differ).\n")
	b.WriteString("- PREFER Drivable=true seats for owners — they can be launched automatically.\n")
	b.WriteString("- Allocate by complexity: assign the more complex tasks to the more capable\n")
	b.WriteString("  seats.\n")
	b.WriteString("- Use a GUI seat (Drivable=false) only when a task truly requires it, and when\n")
	b.WriteString("  you do, note in that task's spec that a human hand-off is needed.\n\n")

	b.WriteString("## Per-task spec files\n")
	fmt.Fprintf(&b, "Write one spec per task to `.pact/tasks/%s-<id>.md` (e.g. `.pact/tasks/%s-<id>.md`).\n", in.Feature, in.Feature)
	b.WriteString("Each spec must contain:\n")
	b.WriteString("- 目标 / Goal — what this task delivers.\n")
	b.WriteString("- 改文件 / Files — the bounded set of files it may touch.\n")
	b.WriteString("- 契约 / Contract — signatures, schema, or API shape.\n")
	b.WriteString("- 验收 / Acceptance — how the reviewer confirms it.\n")
	b.WriteString("- A machine-readable `verify:` line — a single command the harness can run,\n")
	b.WriteString("  e.g. `verify: go test ./internal/<pkg>/` or `verify: npm run -C web test`.\n")
	b.WriteString("  Every task MUST have a `verify:` line.\n\n")

	b.WriteString("## Plan manifest\n")
	fmt.Fprintf(&b, "Write the manifest to `.pact/plan-%s.json` (i.e. `.pact/plan-%s.json`).\n", in.Feature, in.Feature)
	b.WriteString("It MUST match this schema (Plan / PlanTask). Example:\n")
	b.WriteString("```json\n")
	b.WriteString(manifestSchemaExample(in.Feature))
	b.WriteString("\n```\n")
	b.WriteString("Field rules: `verify` is the same machine-readable command as the spec's\n")
	b.WriteString("`verify:` line; `deps` lists the ids this task depends on (forming the serial\n")
	b.WriteString("chain); `spec` points at the `.pact/tasks/` file you wrote; `owner` and\n")
	b.WriteString("`reviewer` are roster seat ids and must differ.\n")

	return b.String()
}

// manifestSchemaExample renders a concrete, schema-correct Plan example so the
// planner agent has every PlanTask field name in front of it.
func manifestSchemaExample(feature string) string {
	if feature == "" {
		feature = "myfeature"
	}
	return fmt.Sprintf(`{
  "feature": %q,
  "branch": %q,
  "tasks": [
    {
      "id": "step1",
      "owner": "<drivable-seat>",
      "reviewer": "<other-seat>",
      "spec": ".pact/tasks/%s-step1.md",
      "verify": "go test ./internal/%s/",
      "deps": []
    },
    {
      "id": "step2",
      "owner": "<seat>",
      "reviewer": "<other-seat>",
      "spec": ".pact/tasks/%s-step2.md",
      "verify": "go test ./internal/%s/",
      "deps": ["step1"]
    }
  ]
}`, feature, "feat-"+feature, feature, feature, feature, feature)
}
