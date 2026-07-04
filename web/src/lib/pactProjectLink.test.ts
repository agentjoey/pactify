import { describe, it, expect } from "vitest";
import { project, type PactEvent } from "@pactify-apps/pact-project";

// Proves the monorepo path-B linking (vite.config.ts resolve.alias +
// tsconfig.app.json paths): the dashboard imports the shared cloud/ projection
// package straight from source and it runs. This is the foundation RelaySource
// (P3) builds on — relay events → project() → State — so if this import ever
// breaks, the alias wiring regressed.
describe("cloud package link: @pactify-apps/pact-project", () => {
  it("projects events into State through the workspace alias", () => {
    const events: PactEvent[] = [
      {
        event_id: "e1",
        ts: "2026-01-01T00:00:00Z",
        agent_id: "claude",
        role: "orchestrator",
        event_type: "init",
        task_id: "",
        feature: "",
        payload: {
          project: "demo",
          seats: [{ id: "claude", roles: ["orchestrator"], kind: "claude-code" }],
        },
      },
      {
        event_id: "e2",
        ts: "2026-01-01T00:01:00Z",
        agent_id: "claude",
        role: "orchestrator",
        event_type: "assign",
        task_id: "t1",
        feature: "f1",
        payload: { branch: "feat-f1", owner: "kimi", reviewer: "claude", spec: "s.md" },
      },
      {
        event_id: "e3",
        ts: "2026-01-01T00:02:00Z",
        agent_id: "kimi",
        role: "worker",
        event_type: "checkpoint",
        task_id: "t1",
        feature: "f1",
        payload: { evidence: "done" },
      },
    ];

    const state = project(events);

    expect(state.project).toBe("demo");
    expect(state.agents.map((a) => a.id)).toContain("claude");
    expect(state.features).toHaveLength(1);
    expect(state.features[0].id).toBe("f1");
    expect(state.features[0].tasks[0].status).toBe("awaiting_review");
    expect(state.awaiting_count).toBe(1);
  });
});
