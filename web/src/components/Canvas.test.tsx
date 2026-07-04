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
import { DataSourceProvider } from "../lib/datasource";

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

const draftProps = {
  drafts: [] as Draft[],
  setDrafts: () => {},
  draftFeatures: [] as DraftFeature[],
  setDraftFeatures: () => {},
};

describe("Canvas", () => {
  beforeEach(() => {
    putLayout.mockClear();
    getLayout.mockReset();
    getLayout.mockResolvedValue({});
  });

  it("lands in Office mode with no mode segment", async () => {
    render(
      <Canvas project="demo" state={fixture} author {...draftProps} />,
    );
    await waitFor(() => expect(screen.getByTestId("office-view")).toBeInTheDocument());
    expect(screen.getByTestId("canvas-toolbar")).toBeInTheDocument();
    expect(screen.queryByTestId("canvas-modeseg")).toBeNull();
  });

  it("New-task editor pre-fills an auto-generated id (not blank)", async () => {
    render(
      <Canvas project="demo" state={fixture} author {...draftProps} />,
    );
    fireEvent.click(await screen.findByRole("button", { name: /Task/ }));
    const idInput = (await screen.findByLabelText("task id")) as HTMLInputElement;
    expect(idInput.value).toBe("t1");
  });

  it("office mode: New Task button opens the TaskEditor", async () => {
    render(
      <Canvas project="demo" state={fixture} author {...draftProps} />,
    );
    await waitFor(() => expect(screen.getByTestId("office-view")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /^.?Task$/ }));
    expect(await screen.findByTestId("task-editor")).toBeInTheDocument();
  });

  it("office mode: New Feature button opens the inline feature form", async () => {
    render(
      <Canvas project="demo" state={fixture} author {...draftProps} />,
    );
    await waitFor(() => expect(screen.getByTestId("office-view")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Feature/ }));
    expect(await screen.findByLabelText("feature id")).toBeInTheDocument();
  });

  it("office mode: empty dock New-task entry opens the TaskEditor", async () => {
    render(
      <Canvas project="demo" state={fixture} author {...draftProps} />,
    );
    await waitFor(() => expect(screen.getByTestId("office-dock")).toBeInTheDocument());
    fireEvent.click(screen.getByTestId("dock-new-task"));
    expect(await screen.findByTestId("task-editor")).toBeInTheDocument();
  });

  it("office mode: a new seat joining does not move existing desks", async () => {
    const { container, rerender } = render(
      <Canvas project="demo" state={fixture} author {...draftProps} />,
    );
    await waitFor(() => expect(container.querySelector('.react-flow__node[data-id="desk:bob"]')).not.toBeNull());
    const bobBefore = (container.querySelector('.react-flow__node[data-id="desk:bob"]') as HTMLElement).style.transform;

    const grown: State = {
      ...fixture,
      agents: [...fixture.agents, { id: "carol", roles: ["worker"] }],
    };
    rerender(
      <Canvas project="demo" state={grown} author {...draftProps} />,
    );
    await waitFor(() => expect(container.querySelector('.react-flow__node[data-id="desk:carol"]')).not.toBeNull());
    const bobAfter = (container.querySelector('.react-flow__node[data-id="desk:bob"]') as HTMLElement).style.transform;
    expect(bobAfter).toBe(bobBefore);
  });

  it("office click-dispatch opens DispatchModal with the seat as owner", async () => {
    render(
      <DataSourceProvider>
        <Canvas
          project="demo"
          state={fixture}
          author
          drafts={[{ id: "d1", specMd: "# d", feature: "F1", deps: [] }]}
          setDrafts={() => {}}
          draftFeatures={[]}
          setDraftFeatures={() => {}}
        />
      </DataSourceProvider>,
    );
    await waitFor(() => expect(screen.getByTestId("desk-bob")).toBeInTheDocument());
    fireEvent.click(screen.getByTestId("desk-bob"));
    const modal = await screen.findByTestId("dispatch-modal");
    expect(modal).toBeInTheDocument();
    expect(modal.textContent).toContain("bob");
    expect(modal.textContent).toContain("d1");
  });
});
