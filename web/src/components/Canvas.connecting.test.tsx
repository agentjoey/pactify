import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { State } from "../lib/types";
import type { Draft, DraftFeature } from "../lib/canvas";

const putLayout = vi.fn().mockResolvedValue(undefined);
const getLayout = vi.fn().mockResolvedValue({});
vi.mock("../lib/api", () => ({
  getLayout: (...args: unknown[]) => getLayout(...args),
  putLayout: (...args: unknown[]) => putLayout(...args),
  getActingSeat: vi.fn().mockResolvedValue({ seat: "" }),
}));

// Force useConnection().inProgress true while keeping the rest of React Flow real
// (Canvas pulls ReactFlow/useReactFlow/etc. from this module). ConnectingFlag
// reports the flag up via onChange and Canvas folds `connecting` into the stage
// className — the assertion below proves that lift survives into the controlled
// className rather than being clobbered by a re-render.
vi.mock("@xyflow/react", async () => {
  const actual = await vi.importActual<typeof import("@xyflow/react")>("@xyflow/react");
  return { ...actual, useConnection: () => ({ inProgress: true }) };
});

import { Canvas } from "./Canvas";

const fixture: State = {
  project: "demo",
  awaiting_count: 0,
  agents: [{ id: "alice", roles: ["orchestrator"] }],
  features: [
    {
      id: "F1",
      branch: "feat/f1",
      status: "active",
      tasks: [{ id: "T1", owner: "alice", status: "assigned", reviewer: "alice", spec: "", evidence: "" }],
    },
  ],
};

const planProps = {
  drafts: [] as Draft[],
  setDrafts: () => {},
  draftFeatures: [] as DraftFeature[],
  setDraftFeatures: () => {},
  initialMode: "plan" as const,
};

describe("Canvas connecting class lift", () => {
  beforeEach(() => {
    putLayout.mockClear();
    getLayout.mockReset();
    getLayout.mockResolvedValue({});
  });

  it("folds `connecting` into the controlled stage className when a connection is in progress", async () => {
    render(<Canvas project="demo" state={fixture} author={true} {...planProps} />);
    await waitFor(() => {
      expect(screen.getByTestId("canvas-root").className).toContain("connecting");
    });
  });
});
