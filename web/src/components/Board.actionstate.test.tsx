import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import type { State } from "../lib/types";
import { Board } from "./Board";
import { DataSourceProvider } from "../lib/datasource";

const fx = (status: string): State => ({
  project: "demo",
  awaiting_count: status === "awaiting_review" ? 1 : 0,
  agents: [
    { id: "alice", roles: ["orchestrator"] },
    { id: "bob", roles: ["worker"] },
  ],
  features: [
    {
      id: "F1",
      branch: "feat/f1",
      status: "active",
      tasks: [{ id: "T1", owner: "bob", status, reviewer: "alice", spec: "", evidence: "" }],
    },
  ],
});

const renderBoard = (state: State) =>
  render(
    <DataSourceProvider>
      <Board state={state} selected="" onSelect={() => {}} project="demo" author onChanged={() => {}} />
    </DataSourceProvider>,
  );

// The engine accepts `accept` and `changes` only from awaiting_review
// (engine.go:878 / :915). The Review column also shows changes_requested cards,
// and their buttons stayed live — so the only way to learn the action was
// illegal was to click it and read the server's rejection. That round trip is
// exactly what a Human Owner hit in the [UI-GATE] incident.
describe("Board — review actions follow task status", () => {
  it("disables Accept and Changes on a changes_requested card", () => {
    renderBoard(fx("changes_requested"));

    expect(screen.getByTestId("card-accept")).toBeDisabled();
    expect(screen.getByTestId("card-changes")).toBeDisabled();
  });

  it("says why they are unavailable instead of leaving a bare dead button", () => {
    renderBoard(fx("changes_requested"));

    const why = screen.getByTestId("card-accept").getAttribute("title") ?? "";
    expect(why).toContain("changes_requested");
    expect(why.length).toBeGreaterThan(0);
  });

  it("keeps both live while the task really is awaiting review", () => {
    renderBoard(fx("awaiting_review"));

    expect(screen.getByTestId("card-accept")).not.toBeDisabled();
    expect(screen.getByTestId("card-changes")).not.toBeDisabled();
  });
});
