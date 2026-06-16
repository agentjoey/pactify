# showcase — dashboard seed project

A hand-authored `.pact/log.jsonl` that projects to a rich state, so the dashboard
(canvas node graph, office desks, ants, seat roster, kanban, replay timeline) has
real data to display and review. **Dev/demo fixture only — not a real project.**

State it produces: 3 seats (claude orchestrator·reviewer, opencode worker, gemini
worker), 2 features (relay, office-ui), 6 tasks spanning every status —
`accepted` (t1) · `awaiting_review` (t2) · `in_progress` (t3) ·
`changes_requested` (t4) · `assigned` (t5, t6) — with dep chains
t1→t2→t3 and t4→{t5,t6}.

## Serve it

```bash
pactify serve --addr 0.0.0.0:7777 --project "$(git rev-parse --show-toplevel)/dev/showcase"
```

`--project` is repeatable and NOT persisted to `~/.pactify/projects.json`, so this
adds the "showcase" project to the dashboard for the session without touching your
real registry. Pick **showcase** in the sidebar.

To regenerate/extend: edit `.pact/log.jsonl` (newline-delimited events; see
`internal/event/event.go` for the schema and `internal/projection/project.go` for
how events fold into state) and re-run `pactify status` here to preview the
projection.
