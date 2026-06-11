import { render, screen, waitFor, fireEvent } from "@testing-library/react";
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

import { Canvas } from "./Canvas";

// Two features so dimming has something to dim: F1's frame is focused, F2 must
// pick up the focus-dim class.
const fixture: State = {
  project: "demo",
  awaiting_count: 0,
  agents: [{ id: "alice", roles: ["orchestrator"] }],
  features: [
    {
      id: "F1",
      branch: "feat/f1",
      status: "active",
      tasks: [{ id: "T1", owner: "alice", status: "accepted", reviewer: "alice", spec: "", evidence: "" }],
    },
    {
      id: "F2",
      branch: "feat/f2",
      status: "active",
      tasks: [{ id: "T2", owner: "alice", status: "assigned", reviewer: "alice", spec: "", evidence: "" }],
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

describe("Canvas feature focus mode", () => {
  beforeEach(() => {
    putLayout.mockClear();
    getLayout.mockReset();
    getLayout.mockResolvedValue({});
  });

  it("clicking a feature header focuses it: chip appears + stage gets focus-active + other feature dims", async () => {
    render(<Canvas project="demo" state={fixture} author={false} {...planProps} />);

    // Wait for the frames to render, then click F1's header.
    const heads = await screen.findAllByTestId("feature-frame-head");
    expect(heads.length).toBe(2);
    const f1Head = heads.find((h) => h.textContent?.includes("F1"))!;
    fireEvent.click(f1Head);

    // The focus chip appears naming F1, and the stage carries focus-active.
    const chip = await screen.findByTestId("focus-chip");
    expect(chip.textContent).toContain("F1");
    expect(screen.getByTestId("canvas-root").className).toContain("focus-active");
    expect(screen.getByTestId("canvas-root").getAttribute("data-focus")).toBe("F1");

    // F2's node (the other feature) gets the focus-dim class; F1's stays lit.
    await waitFor(() => {
      const dimmed = document.querySelectorAll(".react-flow__node.focus-dim");
      expect(dimmed.length).toBeGreaterThan(0);
    });
  });

  it("clicking the same header again toggles focus off", async () => {
    render(<Canvas project="demo" state={fixture} author={false} {...planProps} />);
    const heads = await screen.findAllByTestId("feature-frame-head");
    const f1Head = heads.find((h) => h.textContent?.includes("F1"))!;
    fireEvent.click(f1Head);
    await screen.findByTestId("focus-chip");
    fireEvent.click(f1Head);
    await waitFor(() => expect(screen.queryByTestId("focus-chip")).toBeNull());
    expect(screen.getByTestId("canvas-root").className).not.toContain("focus-active");
  });

  it("Esc exits focus mode", async () => {
    render(<Canvas project="demo" state={fixture} author={false} {...planProps} />);
    const heads = await screen.findAllByTestId("feature-frame-head");
    const f1Head = heads.find((h) => h.textContent?.includes("F1"))!;
    fireEvent.click(f1Head);
    await screen.findByTestId("focus-chip");
    fireEvent.keyDown(window, { key: "Escape" });
    await waitFor(() => expect(screen.queryByTestId("focus-chip")).toBeNull());
  });

  it("the chip ✕ exits focus mode", async () => {
    render(<Canvas project="demo" state={fixture} author={false} {...planProps} />);
    const heads = await screen.findAllByTestId("feature-frame-head");
    const f1Head = heads.find((h) => h.textContent?.includes("F1"))!;
    fireEvent.click(f1Head);
    const chip = await screen.findByTestId("focus-chip");
    fireEvent.click(chip);
    await waitFor(() => expect(screen.queryByTestId("focus-chip")).toBeNull());
  });
});
