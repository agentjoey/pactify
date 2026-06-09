# Pact Protocol v1 — Normative Specification

> Status: Frozen · Version: 1 · Date: 2026-06-09
> Implements: M1.1 Protocol Freeze
> Reference implementation: `.pact/bin/pact.sh`
> Machine-readable schema: `schemas/event.schema.json`

---

## 1. Overview and Version

The pact protocol is a multi-agent coordination protocol for software development. It enables any number of agents — regardless of vendor, runtime, or tool interface — to collaborate on a shared codebase by reading and writing a small set of plain files under a `.pact/` directory inside a git repository. The protocol requires no external services, message brokers, or shared memory: the append-only event log (`log.jsonl`) is the authoritative source of truth, and all state is a deterministic projection of that log.

**Design principles:**

- Agent-agnostic: any agent that can read files and run shell commands can participate.
- Append-only: events are never deleted or mutated; the log grows monotonically.
- Self-contained: the entire protocol state lives inside `.pact/` and travels with the repository.
- Normative rules: two invariants ("the pact") are enforced by all conformant implementations.

### 1.1 protocol_version

`protocol_version` is a single integer. The current value is `1`.

It MUST be recorded in two places:

1. The `payload.protocol_version` field of the `init` event (authoritative — travels with the log).
2. The header of `.pact/PROJECT.md` (human-visible annotation).

### 1.2 Compatibility Rules

These rules are normative and MUST be enforced by all v1 implementations:

1. **Forward compatibility:** A reader MUST ignore unknown fields in a known event envelope and MUST ignore unknown `event_type` values. Implementations MUST NOT fail on encountering either.
2. **Additive changes are non-breaking:** Adding optional fields to the event envelope, adding optional fields to a payload, or introducing new `event_type` values does NOT bump `protocol_version`. Older readers ignore the additions; newer readers use them.
3. **Breaking changes bump the major:** Changing the semantics of an existing field, removing a required field, or altering the state machine constitutes a breaking change and MUST result in a new `protocol_version` integer (e.g., `2`).
4. **Fail closed on higher major:** An implementation MUST fail closed — that is, refuse to operate and surface an upgrade prompt — when it encounters a `protocol_version` in an `init` event that is higher than the major it supports. It MUST NOT attempt best-effort processing of an unsupported major.
5. **Backward compatibility within major:** A v1 implementation reading a log produced by an earlier v1 revision (e.g., one that pre-dates `event_id`) MUST treat absent optional fields as their defined defaults and continue normally.

---

## 2. Event Log (`log.jsonl`)

`log.jsonl` is the authoritative source of truth for all protocol state. It is an append-only file in which each line is a complete, self-contained JSON object representing one event. No line is ever deleted or modified after it is written.

### 2.1 Event Envelope

Every event MUST conform to the following frozen envelope. All eight top-level fields are REQUIRED:

```json
{
  "event_id":   "<opaque string>",
  "ts":         "2026-06-09T10:00:00Z",
  "agent_id":   "claude-opus",
  "role":       "orchestrator",
  "event_type": "assign",
  "task_id":    "T1",
  "feature":    "CLI-INIT",
  "payload":    {}
}
```

**Field semantics:**

| Field | Type | Constraint | Description |
|---|---|---|---|
| `event_id` | string | non-empty, globally unique | Opaque identifier for this event. MUST be a globally-unique string (UUIDv4 or 128-bit random hex). Used for deduplication and referencing; not a sequence number. |
| `ts` | string | UTC ISO-8601 | Timestamp of event creation in UTC ISO-8601 format (e.g., `2026-06-09T10:00:00Z`). |
| `agent_id` | string | `^[a-z0-9][a-z0-9-]*$` | The seat identifier of the agent emitting this event. MUST match the slug pattern. |
| `role` | string | enum | The functional role of the emitter for this event. MUST be one of `orchestrator`, `worker`, `reviewer`. Derived from `event_type` (see §2.3); implementations MUST NOT trust a self-reported `role` that contradicts the `event_type`. |
| `event_type` | string | enum | One of the seven v1 event types (see §2.2). |
| `task_id` | string | | The task identifier this event applies to. MUST be `""` (empty string) when the event is not task-scoped (e.g., `init`, `join`, `merge`). |
| `feature` | string | | The feature identifier this event applies to. MUST be `""` (empty string) when the event is not feature-scoped (e.g., `init`, `join`). |
| `payload` | object | | Event-type-specific data. Required fields per event type are specified in §2.4. Additional fields MAY be present and MUST be ignored by readers that do not recognise them. |

The envelope itself MUST NOT have additional top-level fields beyond these eight in a v1 event. Readers MUST ignore any extra top-level fields encountered (forward compatibility).

The machine-readable definition of the full envelope is `schemas/event.schema.json`.

### 2.2 event_type Enum (v1)

The following seven values constitute the complete v1 `event_type` enum:

`init` | `assign` | `join` | `checkpoint` | `accept` | `changes_requested` | `merge`

Implementations MUST ignore lines whose `event_type` is not in this list (forward compatibility with future additive event types).

### 2.3 Role Derivation

`role` is derived from `event_type` according to the following mapping. It is a denormalised convenience field; conformant implementations MUST apply these mappings:

| event_type | role |
|---|---|
| `init` | `orchestrator` |
| `assign` | `orchestrator` |
| `merge` | `orchestrator` |
| `join` | `worker` |
| `checkpoint` | `worker` |
| `accept` | `reviewer` |
| `changes_requested` | `reviewer` |

### 2.4 Per-event_type Payload Required Fields

Each `payload` object MUST contain at minimum the fields listed below. Additional fields are permitted and MUST be ignored by readers that do not recognise them.

| event_type | Required payload fields |
|---|---|
| `init` | `project` (string), `protocol_version` (integer), `seats` (array of seat objects; see §6 and `schemas/seat.schema.json`), `base_branch` (string) |
| `assign` | `owner` (string, seat slug), `reviewer` (string, seat slug), `branch` (string, git branch name), `spec` (string, path to task file) |
| `join` | `roles` (array of strings) |
| `checkpoint` | `evidence` (string; multi-line allowed; implementations MAY fold newlines when rendering) |
| `accept` | _(empty object `{}` is valid; no required fields)_ |
| `changes_requested` | `reason` (string) |
| `merge` | _(empty object `{}` is valid; no required fields)_ |

### 2.5 Event Ordering

Events are ordered by `ts` (UTC ISO-8601 sort) and, within the same timestamp, by append order within the file. There is no sequence number field. `event_id` provides stable identity for individual events; it is not a monotone counter. This design accommodates git merges that interleave events from multiple branches without requiring coordination on a global counter.

---

## 3. `.pact/` File Contract

A conformant v1 repository MUST maintain the following directory layout inside `.pact/`:

```
.pact/
  PROJECT.md        # Charter + managed seat block (contains protocol_version in header)
  STATE.yml         # Regenerable projection of log.jsonl (not authoritative)
  tasks/<id>.md     # One file per task; YAML frontmatter + markdown body
  log.jsonl         # Authoritative event log (append-only)
  bin/              # Implementation-provided tooling (not part of the wire contract)
```

### 3.1 `log.jsonl`

The authoritative source of truth. All programmatic protocol decisions — including enforcement of the two rules, state machine transitions, and roster checks — MUST read and interpret the log directly. `STATE.yml` MUST NOT be used as the basis for protocol decisions.

### 3.2 `STATE.yml`

A human-readable, regenerable projection of the current state derived from `log.jsonl`. It is produced by replaying the log. The exact rendering format is implementation-defined and is NOT part of the v1 wire contract. Implementations MUST be able to regenerate `STATE.yml` from `log.jsonl` at any time (see §8, Recovery). `STATE.yml` is provided as a convenience for human inspection and for agent cold-start orientation; it MUST NOT be treated as authoritative.

### 3.3 `PROJECT.md`

Contains the project charter and the seat roster in a managed block delimited by `<!-- pact:begin -->` / `<!-- pact:end -->` markers. The `init` verb rewrites the managed block idempotently and MUST NOT overwrite content outside the block. The managed block MUST include a header annotation carrying `protocol_version` (e.g., `# <project> — Pact Charter (protocol_version: 1)`).

### 3.4 `tasks/<id>.md`

One Markdown file per task. It MUST carry a YAML frontmatter block at the top of the file with the following fields (conforming to `schemas/task.schema.json`):

```markdown
---
id: T1
feature: CLI-INIT
owner: opencode
reviewer: claude-opus
---
## Spec
...
## Acceptance
- [ ] ...
## Handoff log
...
```

The frontmatter fields (`id`, `feature`, `owner`, `reviewer`) are a redundant mirror of the corresponding `assign` event fields. This allows an agent to orient itself by reading a single task file without parsing the full log. In the event of a conflict between the task file frontmatter and the log, the log is authoritative.

Machine-readable schema: `schemas/task.schema.json`.

### 3.5 `bin/`

Contains the protocol implementation tooling (e.g., `pact.sh` for the bash reference implementation, or a compiled `pactify` binary). The contents of `bin/` are NOT part of the v1 wire contract and may vary between implementations.

---

## 4. State Machines

### 4.1 Task State Machine

A task MUST progress through the following states. No other transitions are valid:

```
todo → assigned → in_progress → awaiting_review → accepted
                                                 ↘
                                           changes_requested → in_progress
```

- `todo`: task exists in the backlog but has not been assigned via an `assign` event.
- `assigned`: an `assign` event has been emitted for this task; the owner has not yet joined.
- `in_progress`: the owner has joined (via a `join` event) and work is underway; or the task has been returned from `changes_requested`.
- `awaiting_review`: a `checkpoint` event has been emitted; the reviewer must now act.
- `accepted`: an `accept` event has been emitted by the designated reviewer while the task was `awaiting_review`. This is a terminal state.
- `changes_requested`: a `changes_requested` event has been emitted by the designated reviewer while the task was `awaiting_review`. The task returns to `in_progress` for the owner to address.

### 4.2 Feature State Machine

A feature MUST progress through the following states:

```
planned → in_progress → awaiting_review → accepted → shipped
```

- `planned`: feature is declared but has no assigned tasks yet.
- `in_progress`: one or more tasks have been assigned.
- `awaiting_review`: all tasks are `awaiting_review` or `accepted`.
- `accepted`: all tasks are `accepted`.
- `shipped`: a `merge` event has been emitted for this feature (following Rule 2).

---

## 5. The Two Rules (The Pact)

These two rules are the normative invariants of the protocol. Every conformant implementation MUST enforce them. Violations MUST cause the offending command to fail immediately (fail-closed), and the log MUST NOT have an event appended for an operation that violates either rule.

### Rule 1: A Worker Cannot Self-Accept

**A seat MUST NOT self-accept its own work.**

Specifically:

- At `assign` time: `owner` MUST differ from `reviewer`. An implementation MUST reject an `assign` command where `owner == reviewer`.
- At `accept` or `changes_requested` time: only the task's designated `reviewer` (the seat recorded in the `assign` event payload) MAY emit an `accept` or `changes_requested` event for that task. Any other seat MUST be rejected.
- An `accept` or `changes_requested` event is only valid when the task's current state is `awaiting_review`. Attempts to accept or request changes on a task in any other state MUST be rejected.

### Rule 2: A Feature Cannot Merge Until All Its Tasks Are Accepted

**A `merge` event for a feature MUST NOT be emitted unless all tasks belonging to that feature have status `accepted`.**

An implementation MUST check the log projection before emitting a `merge` event and MUST reject the merge command if any task under the feature is not in the `accepted` state.

---

## 6. Seat Identity

### 6.1 Definition

`agent_id` is a **project-local, self-declared, stable seat identifier**. It represents a functional role in the project (e.g., `claude-opus`, `opencode`, `reviewer-gpt4`), not a global identity, a process, or a session. The same seat may be occupied by different agent instances at different times; the seat identifier remains stable.

### 6.2 Roster

The authoritative seat roster is the `seats` array in the `payload` of the `init` event. Seats are declared at init; a `join` event does NOT introduce a new seat — it only signals that an already-declared seat has come online (cold-start / resume). An `agent_id` that appears in the log but is not a declared seat is a validation error. (Adding seats after init is deferred to a future protocol revision.)

Each seat object MUST conform to `schemas/seat.schema.json`:

```json
{ "id": "claude-opus", "roles": ["reviewer"], "entry": "CLAUDE.md" }
```

- `id` MUST match the slug pattern `^[a-z0-9][a-z0-9-]*$`.
- `roles` MUST be a non-empty array; valid values are `orchestrator`, `worker`, `reviewer`.
- `entry` is the repo-relative path to the agent's vendor entry file (e.g., `CLAUDE.md`, `AGENTS.md`).
- `id` values MUST be unique within the roster. Duplicate seat ids are a validation error.

### 6.3 Separation of Duties

Separation of duties between worker and reviewer is enforced by **requiring distinct seat names** — the owner and reviewer of any task MUST be different seat ids. This is a naming constraint, not a cryptographic guarantee. Cryptographic authentication of seat identity is out of scope for v1.

### 6.4 task_id Uniqueness

`task_id` MUST be globally unique across all features within a project. Implementations MUST reject an `assign` command if the `task_id` already exists in the log, regardless of which feature it belongs to. Conformance checks MUST verify this invariant.

### 6.5 Handshake

A seat learns its own `agent_id` from its vendor entry file (e.g., `CLAUDE.md` or `AGENTS.md`). This file is baked by `init` for each seat listed in the roster. The file contains a managed block with the `PACT_AGENT_ID` export and the join command.

All implementations MUST fail closed — immediately refuse to execute any pact command — if the `PACT_AGENT_ID` environment variable is unset or empty.

---

## 7. Command Verbs

The following verbs constitute the frozen v1 command contract. The verb names and their required-parameter semantics MUST be consistent across all conformant implementations.

Two skins implement the same contract: `pact_<verb>` (bash, reference implementation) and `pactify <verb>` (Go CLI, M1.2). Implementations MAY add optional `--flags` (non-breaking); they MUST NOT rename verbs or change required-parameter semantics.

### 7.1 Verb Reference

```
init   --project <name> --seat "<id>:<roles>:<entry>" [--seat ...]
           Scaffolds .pact/, bakes vendor entry files, emits init event.
           Required: --project, at least one --seat.
           Seat format: id:comma-separated-roles:entry-path (exactly 3 fields).

join   <id> --roles <r1,r2>
           Worker cold-start (or resume after crash). Emits join event.
           Required: seat id positional arg, --roles.

assign <task_id> --feature <feature_id> --branch <branch_name> \
         --owner <seat_id> --reviewer <seat_id> [--spec <path>]
           Emits assign event; creates tasks/<task_id>.md.
           Required: task_id, --feature, --branch, --owner, --reviewer.
           Enforces Rule 1 (owner != reviewer) at call time.
           Enforces task_id global uniqueness at call time.

checkpoint <task_id> --evidence "<text>"
           Transitions task to awaiting_review. Emits checkpoint event.
           Required: task_id, --evidence.
           MUST be called by the task owner (PACT_AGENT_ID == task.owner).

accept <task_id>
           Transitions task to accepted. Emits accept event.
           Required: task_id.
           MUST be called by the task reviewer (PACT_AGENT_ID == task.reviewer).
           Task MUST be in awaiting_review state.

changes <task_id> --reason "<text>"
           Transitions task to changes_requested (then back to in_progress).
           Emits changes_requested event.
           Required: task_id, --reason.
           MUST be called by the task reviewer (PACT_AGENT_ID == task.reviewer).
           Task MUST be in awaiting_review state.

merge  <feature_id>
           Transitions feature to shipped. Emits merge event.
           Required: feature_id.
           Enforces Rule 2 (all tasks accepted) at call time.

status
           Prints the current STATE.yml to stdout. Read-only.

log    [--replay]
           Without --replay: prints log.jsonl to stdout.
           With --replay: rebuilds STATE.yml from log.jsonl (reconciliation).

validate
           Checks: (a) STATE.yml matches a fresh render of the log (no drift);
           (b) all agent_ids in the log are declared seats;
           (c) all seat ids match the slug pattern;
           (d) no task has owner == reviewer (Rule 1);
           (e) task_ids are globally unique;
           (f) protocol_version in the log does not exceed the supported major
               (fail-closed if higher);
           (g) every event carries a non-empty event_id.
           Exits non-zero if any check fails.

help
           Prints the verb reference to stdout.
```

---

## 8. Recovery

The log is the source; `STATE.yml` is a projection. They can diverge only through bugs, manual edits, or aborted writes.

**Reconciliation:** Running `log --replay` (or `pactify log --replay`) rebuilds `STATE.yml` by replaying the log from the beginning. This MUST produce a deterministic result regardless of how many times it is run.

**Crashed worker:** A worker that terminates unexpectedly resumes by re-joining the same seat:

```bash
export PACT_AGENT_ID=<seat_id>
source .pact/bin/pact.sh && pact_join <seat_id> --roles <roles>
```

The `join` event is idempotent at the state-machine level: re-joining a seat that already has `in_progress` tasks does not change their state. The worker then re-reads `STATE.yml` to locate its in-progress tasks and continues.

**Log integrity:** The log MUST NOT be edited manually. If log corruption is suspected, `validate` MUST be run to surface the inconsistency. Recovery from corrupted log entries is outside the scope of v1.

---

## 9. What Is Implementation-Defined

The following aspects of an implementation are explicitly NOT part of the v1 wire contract. They may vary between conformant implementations without affecting interoperability:

1. **Git branch and commit choreography:** The reference implementation (`pact.sh`) performs specific git operations at `join` (branch switch), `checkpoint` (git commit), and `merge` (no-ff merge to base branch). These behaviours are implementation-defined. Alternative implementations may use worktrees, shared trees, cloud-hosted repositories, or no git choreography at all, provided the event log and file contract are honoured.

2. **`STATE.yml` exact render format:** The structure of `STATE.yml` is documented (project, agents, features, tasks, statuses) but the precise YAML layout — field order, indentation, quoting — is implementation-defined. A v1-conformant implementation MUST be able to regenerate `STATE.yml` from the log; it need not match the reference rendering byte-for-byte.

3. **Transport and serve:** How the protocol is exposed — MCP server endpoints, web dashboard, cloud relay, webhook — is outside the v1 contract (planned for M1.3 and beyond).

4. **Schema validation library:** Whether an implementation uses a JSON Schema validation library at runtime (e.g., to validate events against `schemas/event.schema.json` on write) is implementation-defined. v1 requires only that emitted events conform to the schema in practice.

5. **`event_id` generation algorithm:** Implementations MAY use UUIDv4, 128-bit random hex, or another globally-unique generation strategy. The requirement is uniqueness and non-emptiness; the format is opaque to readers.

---

## Conformance Statement

Conformance: an implementation is v1-conformant if its emitted log.jsonl validates against schemas/event.schema.json, it enforces the two rules and the state machines, and it fails closed on a higher protocol major.
