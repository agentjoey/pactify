import { render, screen, cleanup } from "@testing-library/react";
import { describe, it, expect, afterEach } from "vitest";
import type { State } from "../lib/types";
import { DataSourceProvider } from "../lib/datasource";
import { Skeleton, BoardSkeleton, CanvasSkeleton, OpsSkeleton } from "./Skeleton";
import { Board } from "./Board";

afterEach(cleanup);

const emptyState: State = { project: "demo", agents: [], features: [], awaiting_count: 0 };
const loadedState: State = {
  project: "demo",
  awaiting_count: 0,
  agents: [{ id: "alice", roles: ["orchestrator"] }],
  features: [
    {
      id: "F1",
      branch: "b",
      status: "active",
      tasks: [{ id: "T1", owner: "alice", status: "assigned", reviewer: "alice", spec: "", evidence: "" }],
    },
  ],
};

describe("Skeleton primitives", () => {
  it("Skeleton renders the shimmer block with the .skeleton class", () => {
    render(<Skeleton width={40} height={8} />);
    const el = screen.getByTestId("skeleton");
    expect(el.className).toContain("skeleton");
    expect(el.getAttribute("aria-hidden")).toBe("true");
  });

  it("the composite skeletons render their containers", () => {
    const { unmount } = render(<BoardSkeleton />);
    expect(screen.getByTestId("board-skeleton")).toBeTruthy();
    unmount();
    render(<CanvasSkeleton />);
    expect(screen.getByTestId("canvas-skeleton")).toBeTruthy();
    cleanup();
    render(<OpsSkeleton />);
    expect(screen.getByTestId("ops-skeleton")).toBeTruthy();
  });
});

describe("Board first-load skeleton", () => {
  it("shows the skeleton ONLY while loading (pre-snapshot)", () => {
    render(
      <DataSourceProvider>
        <Board state={emptyState} selected="" onSelect={() => {}} loading />
      </DataSourceProvider>,
    );
    expect(screen.getByTestId("board-skeleton")).toBeTruthy();
    // the real columns are not rendered yet
    expect(screen.queryByText("ASSIGNED", { exact: false })).toBeNull();
  });

  it("renders the real board (no skeleton) once a snapshot has landed", () => {
    render(
      <DataSourceProvider>
        <Board state={loadedState} selected="" onSelect={() => {}} loading={false} />
      </DataSourceProvider>,
    );
    expect(screen.queryByTestId("board-skeleton")).toBeNull();
    // a real card id shows
    expect(screen.getAllByText("T1").length).toBeGreaterThan(0);
  });
});
