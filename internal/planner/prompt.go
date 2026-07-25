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
	// Role is the machine-level role profile this seat is bound to (advisory
	// routing config, not protocol state); "" when the seat is unbound. Kind and
	// Model spell out what that role actually launches, so the planner can match
	// a task's nature to a capable seat instead of guessing from the seat id.
	Role  string
	Kind  string
	Model string
}

// RoleInfo is one entry of the machine's role catalog: a named (agent, model)
// profile and the seats currently bound to it. A role with no bound seats is
// still listed so the planner can name it and the human sees what to bind.
type RoleInfo struct {
	Name       string
	Kind       string
	Model      string
	BoundSeats []string
}

// PromptInput is everything the caller gathers to scaffold the planning prompt.
type PromptInput struct {
	Goal     string     // what the user wants delivered
	Feature  string     // feature id / branch slug the plan targets
	RepoTree string     // top-level + key directory tree text (caller-collected)
	Seats    []SeatInfo // roster the plan may assign owners/reviewers from
	// RoleCatalog is the machine's role profiles (empty when none configured, in
	// which case the prompt omits the whole routing section and behaves as before).
	RoleCatalog []RoleInfo
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
		line := fmt.Sprintf("- %s · roles: [%s] · %s", s.ID, strings.Join(s.Roles, ", "), drivable)
		if s.Role != "" {
			line += fmt.Sprintf(" · role: %s (%s", s.Role, s.Kind)
			if s.Model != "" {
				line += " / " + s.Model
			}
			line += ")"
		}
		fmt.Fprintf(&b, "%s\n", line)
	}
	b.WriteString("\n")

	if len(in.RoleCatalog) > 0 {
		b.WriteString("## Role catalog (what each role actually launches)\n")
		for _, r := range in.RoleCatalog {
			line := fmt.Sprintf("- %s → %s", r.Name, r.Kind)
			if r.Model != "" {
				line += " / " + r.Model
			}
			if len(r.BoundSeats) > 0 {
				line += " · seats: " + strings.Join(r.BoundSeats, ", ")
			} else {
				line += " · (no seat is bound to this role yet)"
			}
			fmt.Fprintf(&b, "%s\n", line)
		}
		b.WriteString("\n")

		b.WriteString("## Role routing\n")
		b.WriteString("- For EACH task, name the kind of work it is (its `role`): e.g. frontend,\n")
		b.WriteString("  backend, test — use a role name from the catalog above when one fits.\n")
		b.WriteString("- Put that name in the task's `role` field of the manifest, and PREFER an\n")
		b.WriteString("  owner seat bound to that role — that is what routes visual work to a\n")
		b.WriteString("  capable model and mechanical work to a cheap one.\n")
		b.WriteString("- If the fitting role has NO bound seat, still name the role and pick the\n")
		b.WriteString("  best available seat: the human reviewing the plan will see the gap.\n")
		b.WriteString("- The role is advisory routing metadata. It never overrides the two rules\n")
		b.WriteString("  (owner ≠ reviewer; a feature merges only when every task is accepted).\n\n")
	}

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
	b.WriteString("  you do, note in that task's spec that a human hand-off is needed.\n")
	b.WriteString("- You MAY propose NEW seats when the goal needs a capability no rostered seat\n")
	b.WriteString("  provides. Declare each new seat in the manifest's optional `seats` array as\n")
	b.WriteString("  `{ \"id\": <kebab-slug>, \"kind\": <agent-kind>, \"roles\": [\"worker\"] }`, then you\n")
	b.WriteString("  may use its id as a task owner/reviewer. Apply auto-registers (joins) each\n")
	b.WriteString("  proposed seat with its kind. PREFER existing seats; propose one only when\n")
	b.WriteString("  necessary, and give it a real drivable `kind`.\n\n")

	b.WriteString("## Review dimensions\n")
	b.WriteString("Assign every task ONE review dimension from this set:\n")
	b.WriteString("- correctness — does the change behave as specified and not break existing behavior?\n")
	b.WriteString("- security — does the change avoid new attack surface or data leaks?\n")
	b.WriteString("- performance — does the change meet latency, throughput, or resource targets?\n")
	b.WriteString("- maintainability — is the change readable, tested, and consistent with the codebase?\n")
	b.WriteString("- ux — does the change serve the end user or operator as intended?\n")
	b.WriteString("Write the chosen dimension into the manifest task's `dimension` field, and\n")
	b.WriteString("state the dimension explicitly in that task's spec under 验收 / Acceptance.\n\n")

	b.WriteString("## Per-task spec files\n")
	fmt.Fprintf(&b, "Write one spec per task to `.pact/tasks/%s-<id>.md` (e.g. `.pact/tasks/%s-<id>.md`).\n", in.Feature, in.Feature)
	b.WriteString("Each spec must contain:\n")
	b.WriteString("- 目标 / Goal — what this task delivers.\n")
	b.WriteString("- 改文件 / Files — the bounded set of files it may touch.\n")
	b.WriteString("- 契约 / Contract — signatures, schema, or API shape.\n")
	b.WriteString("- 验收 / Acceptance — how the reviewer confirms it, including the review dimension.\n")
	b.WriteString("- A machine-readable `verify:` line — a single command the harness can run,\n")
	b.WriteString("  e.g. `verify: go test ./internal/<pkg>/` or `verify: npm run -C web test`.\n")
	b.WriteString("  Every task MUST have a `verify:` line.\n\n")

	b.WriteString("## Naming rules (REQUIRED — ids are validated and rejected if violated)\n")
	b.WriteString("- feature id: kebab-case, lowercase, a short verb phrase describing the goal\n")
	b.WriteString("  (e.g. \"add-2fa-login\"), max 40 chars, matching ^[a-z0-9][a-z0-9-]*$.\n")
	b.WriteString("- task id: kebab-case slug, ideally <feature>-<step> (e.g. \"add-2fa-otp-gen\"),\n")
	b.WriteString("  matching ^[a-z0-9][a-z0-9-]*$, unique within the feature.\n\n")

	b.WriteString("\nVerify rules (REQUIRED — each task's verify must be SCOPED to that task):\n")
	b.WriteString("- The verify command must validate THIS task's change, runnable headless, and\n")
	b.WriteString("  must NOT fail on pre-existing debt in files this task does not touch.\n")
	b.WriteString("- Prefer `test`/`build` gates (a broken import fails them). Do NOT use a\n")
	b.WriteString("  whole-repo linter (e.g. `eslint .`) as a task verify — it trips on unrelated\n")
	b.WriteString("  pre-existing issues the task owner must not fix. Lint only the touched files if at all.\n")
	b.WriteString("- You MAY scope a verify to the changed files with the `{files}` placeholder,\n")
	b.WriteString("  e.g. `verify: npx eslint {files}`. An empty change set skips the gate.\n")
	b.WriteString("- The owner must never modify out-of-scope files just to satisfy a verify.\n\n")

	b.WriteString("## Plan manifest\n")
	fmt.Fprintf(&b, "Write the manifest to `.pact/plan-%s.json` (i.e. `.pact/plan-%s.json`).\n", in.Feature, in.Feature)
	b.WriteString("It MUST match this schema (Plan / PlanTask). Example:\n")
	b.WriteString("```json\n")
	b.WriteString(manifestSchemaExample(in.Feature))
	b.WriteString("\n```\n")
	b.WriteString("Field rules: `verify` is the same machine-readable command as the spec's\n")
	b.WriteString("`verify:` line; `deps` lists the ids this task depends on (forming the serial\n")
	b.WriteString("chain); `spec` points at the `.pact/tasks/` file you wrote; `owner` and\n")
	b.WriteString("`reviewer` are roster seat ids (or a proposed new seat's id) and must differ.\n")
	b.WriteString("Optional top-level `seats`: an array of proposed NEW seats, each\n")
	b.WriteString("`{ \"id\": <slug>, \"kind\": <agent-kind>, \"roles\": [\"worker\"] }` — omit it when the\n")
	b.WriteString("existing roster suffices.\n")

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
      "deps": [],
      "dimension": "correctness"
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
