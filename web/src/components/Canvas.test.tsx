import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { State } from "../lib/types";

// Mock the api module: getLayout resolves empty, putLayout is a spy, and
// getActingSeat is unused by Canvas but stubbed for completeness.
const putLayout = vi.fn().mockResolvedValue(undefined);
vi.mock("../lib/api", () => ({
  getLayout: vi.fn().mockResolvedValue({}),
  putLayout: (...args: unknown[]) => putLayout(...args),
  getActingSeat: vi.fn().mockResolvedValue({ seat: "" }),
}));

import { Canvas } from "./Canvas";

const fixture: State = {
  project: "demo",
  awaiting_count: 0,
  agents: [
    { id: "alice", roles: ["orchestrator"] },
    { id: "bob", roles: ["worker"] },
  ],
  features: [
    {
      id: "F1",
      branch: "feat/f1",
      status: "active",
      tasks: [
        { id: "T1", owner: "bob", status: "accepted", reviewer: "alice", spec: "", evidence: "" },
        { id: "T2", owner: "bob", status: "assigned", reviewer: "alice", spec: "", evidence: "", deps: ["T1"] },
      ],
    },
  ],
};

describe("Canvas", () => {
  beforeEach(() => {
    putLayout.mockClear();
  });

  it("renders task nodes, seat rail, and the dep edge", async () => {
    const { container } = render(
      <Canvas project="demo" state={fixture} author={false} />,
    );

    // Task node ids appear.
    await waitFor(() => {
      expect(screen.getByText("T1")).toBeInTheDocument();
      expect(screen.getByText("T2")).toBeInTheDocument();
    });

    // Seat rail cards.
    expect(screen.getByText("alice")).toBeInTheDocument();
    expect(screen.getByText("bob")).toBeInTheDocument();

    // Feature group header.
    expect(screen.getByText("F1")).toBeInTheDocument();

    // Dep edge: React Flow renders edges as DOM elements with a data-id.
    await waitFor(() => {
      expect(container.querySelector('[data-id="dep:T1-T2"]')).not.toBeNull();
    });
  });
});
