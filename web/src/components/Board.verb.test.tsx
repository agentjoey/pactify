import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { State } from "../lib/types";
import { Board } from "./Board";
import { postVerb } from "../lib/api";
import { DataSourceProvider } from "../lib/datasource";

vi.mock("../lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../lib/api")>()),
  postVerb: vi.fn(),
}));

const mockPostVerb = vi.mocked(postVerb);

const fx = (t1Status: string): State => ({
  project: "demo",
  awaiting_count: t1Status === "awaiting_review" ? 1 : 0,
  agents: [
    { id: "alice", roles: ["orchestrator"] },
    { id: "bob", roles: ["worker"] },
  ],
  features: [
    {
      id: "F1",
      branch: "feat/f1",
      status: "active",
      tasks: [{ id: "T1", owner: "bob", status: t1Status, reviewer: "alice", spec: "", evidence: "" }],
    },
  ],
});

const renderBoard = (state: State, onChanged = () => {}) =>
  render(
    <DataSourceProvider>
      <Board state={state} selected="" onSelect={() => {}} project="demo" author onChanged={onChanged} />
    </DataSourceProvider>,
  );

describe("Board — inline accept error handling", () => {
  beforeEach(() => mockPostVerb.mockReset());

  it("a failed accept surfaces a danger Alert and re-enables the button", async () => {
    mockPostVerb.mockRejectedValueOnce(new Error("accept: task T1 is not awaiting review"));
    const onChanged = vi.fn();
    renderBoard(fx("awaiting_review"), onChanged);

    fireEvent.click(screen.getByTestId("card-accept"));

    const strip = await screen.findByTestId("board-verb-error");
    expect(strip).toHaveTextContent("Action failed");
    expect(strip).toHaveTextContent("not awaiting review");
    expect(screen.getByTestId("alert").getAttribute("data-tone")).toBe("danger");
    // Error path re-enables (retry is the fix), and no refresh was requested.
    expect(screen.getByTestId("card-accept")).not.toBeDisabled();
    expect(onChanged).not.toHaveBeenCalled();
  });

  it("Dismiss clears the error strip", async () => {
    mockPostVerb.mockRejectedValueOnce(new Error("boom"));
    renderBoard(fx("awaiting_review"));

    fireEvent.click(screen.getByTestId("card-accept"));
    await screen.findByTestId("board-verb-error");

    fireEvent.click(screen.getByText("Dismiss"));
    expect(screen.queryByTestId("board-verb-error")).toBeNull();
  });

  it("Accept disables on click and STAYS disabled after success until the state refresh", async () => {
    let resolve!: () => void;
    mockPostVerb.mockReturnValueOnce(new Promise<void>((r) => { resolve = r; }));
    const onChanged = vi.fn();
    const { rerender } = renderBoard(fx("awaiting_review"), onChanged);

    const btn = screen.getByTestId("card-accept");
    fireEvent.click(btn);
    expect(btn).toBeDisabled();
    // Double click while in flight: the disabled button swallows it.
    fireEvent.click(btn);
    expect(mockPostVerb).toHaveBeenCalledTimes(1);

    resolve();
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    // Success does NOT re-enable — the card only leaves review once the
    // refreshed state lands, and that window is the double-submit hole.
    expect(screen.getByTestId("card-accept")).toBeDisabled();

    // Refresh arrives: T1 accepted, the card leaves review and pending clears.
    rerender(
      <DataSourceProvider>
        <Board state={fx("accepted")} selected="" onSelect={() => {}} project="demo" author onChanged={onChanged} />
      </DataSourceProvider>,
    );
    expect(screen.queryByTestId("card-accept")).toBeNull();

    // If T1 ever re-enters review (changes → re-review), Accept is live again.
    rerender(
      <DataSourceProvider>
        <Board state={fx("awaiting_review")} selected="" onSelect={() => {}} project="demo" author onChanged={onChanged} />
      </DataSourceProvider>,
    );
    expect(screen.getByTestId("card-accept")).not.toBeDisabled();
  });
});
