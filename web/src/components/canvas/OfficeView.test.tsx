import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import type { State } from "../../lib/types";
import type { Draft, LayoutJSON } from "../../lib/canvas";
import { OfficeView } from "./OfficeView";

// Fixture exercising every desk status:
//   bob  → owns T1 in_progress         ⇒ BUSY
//   alice → reviewer of T2 awaiting    ⇒ REVIEW DUE (also owns the in-flight T2)
//   carol → owns T3 awaiting_review    ⇒ WAITING (output parked at reviewer)
//   dave → nothing                     ⇒ IDLE
// shipped: T0 accepted.
const fixture: State = {
  project: "demo",
  awaiting_count: 1,
  agents: [
    { id: "bob", roles: ["worker"] },
    { id: "alice", roles: ["orchestrator", "reviewer"] },
    { id: "carol", roles: ["worker"] },
    { id: "dave", roles: ["worker"] },
  ],
  features: [
    {
      id: "F1",
      branch: "feat/f1",
      status: "active",
      tasks: [
        { id: "T0", owner: "bob", status: "accepted", reviewer: "alice", spec: ".pact/tasks/T0.md", evidence: "" },
        { id: "T1", owner: "bob", status: "in_progress", reviewer: "alice", spec: "", evidence: "" },
        { id: "T2", owner: "bob", status: "awaiting_review", reviewer: "alice", spec: "", evidence: "" },
        { id: "T3", owner: "carol", status: "awaiting_review", reviewer: "alice", spec: "", evidence: "" },
      ],
    },
  ],
};

const noop = () => {};
const baseProps = {
  state: fixture,
  layout: {} as LayoutJSON,
  author: true,
  drafts: [] as Draft[],
  onSaveOffice: noop,
  onSelectTask: noop,
  onDispatchDraft: noop,
};

describe("OfficeView", () => {
  it("renders one desk per joined seat with the right status badge", async () => {
    render(<OfficeView {...baseProps} />);
    await waitFor(() => expect(screen.getByTestId("desk-bob")).toBeInTheDocument());

    expect(screen.getByTestId("desk-status-bob").textContent).toContain("BUSY");
    expect(screen.getByTestId("desk-status-alice").textContent).toContain("REVIEW DUE");
    expect(screen.getByTestId("desk-status-carol").textContent).toContain("WAITING");
    expect(screen.getByTestId("desk-status-dave").textContent).toContain("IDLE");
  });

  it("dims the idle desk and shows the drop hint", async () => {
    const { container } = render(<OfficeView {...baseProps} />);
    await waitFor(() => expect(screen.getByTestId("desk-dave")).toBeInTheDocument());
    const dave = container.querySelector('[data-testid="desk-dave"]')!;
    expect(dave.className).toContain("idle");
    expect(dave.textContent).toContain("拖一个任务到这张桌子即派发");
  });

  it("shows zone counts and parcels in the right zones", async () => {
    render(<OfficeView {...baseProps} />);
    await waitFor(() => expect(screen.getByTestId("desk-bob")).toBeInTheDocument());
    // bob's doing zone holds T1 (in_progress); waiting-on holds T2 (own awaiting).
    const bob = screen.getByTestId("desk-bob");
    expect(bob.textContent).toContain("手上 · doing");
    expect(screen.getAllByTestId("parcel-T1").length).toBeGreaterThan(0);
    // alice's inbox holds T2 (she reviews it).
    const alice = screen.getByTestId("desk-alice");
    expect(alice.textContent).toContain("收件 · inbox");
  });

  it("wall chart shows per-feature progress and tray shows shipped parcels", async () => {
    render(<OfficeView {...baseProps} />);
    const wall = await screen.findByTestId("office-wall");
    expect(wall.textContent).toContain("F1");
    expect(wall.textContent).toContain("1/4"); // 1 accepted of 4 tasks
    const tray = screen.getByTestId("office-tray");
    expect(tray.textContent).toContain("T0");
  });

  it("clicking a parcel selects the task", async () => {
    const onSelectTask = vi.fn();
    render(<OfficeView {...baseProps} onSelectTask={onSelectTask} />);
    await waitFor(() => expect(screen.getAllByTestId("parcel-T1").length).toBeGreaterThan(0));
    fireEvent.click(screen.getAllByTestId("parcel-T1")[0]);
    expect(onSelectTask).toHaveBeenCalledWith("T1");
  });

  it("desk position comes from the office layout key when present", async () => {
    const layout: LayoutJSON = { positions: { "task:X": { x: 9, y: 9 } }, office: { bob: { x: 777, y: 333 } } };
    const { container } = render(<OfficeView {...baseProps} layout={layout} />);
    await waitFor(() => expect(screen.getByTestId("desk-bob")).toBeInTheDocument());
    const node = container.querySelector('.react-flow__node[data-id="desk:bob"]') as HTMLElement;
    expect(node).not.toBeNull();
    // RF applies the position via transform; assert the saved coords flow through.
    expect(node.style.transform).toContain("777");
  });

  it("does not persist on mount (drag-stop is the only save path)", async () => {
    // The office-key-only persistence invariant lives in mergeOfficePos unit
    // tests + the Canvas round-trip; here we only assert mount never auto-saves.
    const onSaveOffice = vi.fn();
    render(<OfficeView {...baseProps} onSaveOffice={onSaveOffice} />);
    await waitFor(() => expect(screen.getByTestId("desk-bob")).toBeInTheDocument());
    expect(onSaveOffice).not.toHaveBeenCalled();
  });

  it("author+live shows the draft dock; replay hides it and disables dispatch", async () => {
    const onDispatchDraft = vi.fn();
    const drafts: Draft[] = [{ id: "d1", specMd: "# d", feature: "F1", deps: [] }];

    // author + live → dock visible.
    const { rerender } = render(
      <OfficeView {...baseProps} drafts={drafts} onDispatchDraft={onDispatchDraft} />,
    );
    await waitFor(() => expect(screen.getByTestId("office-dock")).toBeInTheDocument());
    expect(screen.getByTestId("dock-d1")).toBeInTheDocument();

    // replay → dock gone, clicking an idle desk does NOT dispatch.
    rerender(
      <OfficeView {...baseProps} drafts={drafts} replaying onDispatchDraft={onDispatchDraft} />,
    );
    await waitFor(() => expect(screen.queryByTestId("office-dock")).toBeNull());
    fireEvent.click(screen.getByTestId("desk-dave"));
    expect(onDispatchDraft).not.toHaveBeenCalled();
  });

  it("click-dispatch on an IDLE desk pre-fills owner when exactly one draft exists", async () => {
    const onDispatchDraft = vi.fn();
    const drafts: Draft[] = [{ id: "d1", specMd: "# d", feature: "F1", deps: [] }];
    render(<OfficeView {...baseProps} drafts={drafts} onDispatchDraft={onDispatchDraft} />);
    await waitFor(() => expect(screen.getByTestId("desk-dave")).toBeInTheDocument());
    fireEvent.click(screen.getByTestId("desk-dave"));
    expect(onDispatchDraft).toHaveBeenCalledWith(drafts[0], "dave");
  });

  it("click-dispatch is IDLE-only — clicking a BUSY desk does not dispatch", async () => {
    // Regression for the file header's "idle desks double as the drop affordance"
    // contract: with one draft present, clicking the IDLE dave dispatches, but
    // clicking the BUSY bob (owns the in-flight T1) must NOT.
    const onDispatchDraft = vi.fn();
    const drafts: Draft[] = [{ id: "d1", specMd: "# d", feature: "F1", deps: [] }];
    render(<OfficeView {...baseProps} drafts={drafts} onDispatchDraft={onDispatchDraft} />);
    await waitFor(() => expect(screen.getByTestId("desk-bob")).toBeInTheDocument());

    fireEvent.click(screen.getByTestId("desk-bob"));
    expect(onDispatchDraft).not.toHaveBeenCalled();

    // Sanity: the idle desk still dispatches under the same props.
    fireEvent.click(screen.getByTestId("desk-dave"));
    expect(onDispatchDraft).toHaveBeenCalledWith(drafts[0], "dave");
  });

  it("pane drop resolves the desk even when the pointer lands on its status badge", async () => {
    // Regression for the prefix-collision bug: closest() matches self-first, so a
    // drop whose elementFromPoint is the `desk-status-<seat>` BADGE must still
    // resolve to its parent desk (via the dedicated data-desk-id attribute), not
    // silently die. Stub elementFromPoint to return bob's status badge element.
    const onDispatchDraft = vi.fn();
    const drafts: Draft[] = [{ id: "d1", specMd: "# d", feature: "F1", deps: [] }];
    render(<OfficeView {...baseProps} drafts={drafts} onDispatchDraft={onDispatchDraft} />);
    await waitFor(() => expect(screen.getByTestId("office-view")).toBeInTheDocument());

    const badge = screen.getByTestId("desk-status-bob");
    const prev = document.elementFromPoint;
    document.elementFromPoint = vi.fn().mockReturnValue(badge);
    try {
      const pane = screen.getByTestId("office-view");
      const dataTransfer = { getData: vi.fn().mockReturnValue("d1"), dropEffect: "" };
      fireEvent.drop(pane, { clientX: 10, clientY: 10, dataTransfer });
    } finally {
      document.elementFromPoint = prev;
    }
    expect(onDispatchDraft).toHaveBeenCalledWith(drafts[0], "bob");
  });

  it("renders the transit overlay when a pulsed task changes to awaiting_review", async () => {
    // The lane + carrier ant now live INSIDE <ReactFlow> (viewport-space). jsdom
    // won't run the SMIL motion, but the office-transit element render is
    // assertable. Seed prevStatus with T1 in_progress, then rerender with T1
    // flipped to awaiting_review + a pulse → a lane (owner→reviewer) appears.
    const seed: State = JSON.parse(JSON.stringify(fixture));
    const { rerender } = render(
      <OfficeView {...baseProps} state={seed} pulses={new Set()} />,
    );
    await waitFor(() => expect(screen.getByTestId("desk-bob")).toBeInTheDocument());
    expect(screen.queryByTestId("office-transit")).toBeNull();

    const next: State = JSON.parse(JSON.stringify(fixture));
    next.features[0].tasks[1].status = "awaiting_review"; // T1 bob → alice
    rerender(<OfficeView {...baseProps} state={next} pulses={new Set(["T1"])} />);
    await waitFor(() => expect(screen.getByTestId("office-transit")).toBeInTheDocument());
  });
});
