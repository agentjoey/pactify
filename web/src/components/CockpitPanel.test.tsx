import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { CockpitPanel } from "./CockpitPanel";
import { DataSourceProvider } from "../lib/datasource";
import type { DataSource } from "../lib/datasource";
import type { CockpitStatus, CockpitEvent } from "../lib/api";
import type { Seat, State } from "../lib/types";

let lastSub: FakeSub | null = null;

class FakeSub {
  cb: ((e: CockpitEvent) => void) | null = null;
  closed = false;
  constructor(cb: (e: CockpitEvent) => void) {
    this.cb = cb;
    lastSub = this;
  }
  emit(e: CockpitEvent) {
    this.cb?.(e);
  }
  close() {
    this.closed = true;
  }
}

const defaultAgents: Seat[] = [
  { id: "claude", roles: ["orchestrator"], kind: "claude-code" },
  { id: "kimi", roles: ["worker"], kind: "kimi-cli" },
];

const sampleState: State = {
  project: "p1",
  agents: defaultAgents,
  features: [
    {
      id: "feat-rh",
      branch: "feat-rh",
      status: "in_progress",
      tasks: [
        { id: "t-cfg", owner: "claude", reviewer: "claude", status: "accepted", spec: "", evidence: "" },
        { id: "t-impl", owner: "claude", reviewer: "claude", status: "awaiting_review", spec: "", evidence: "" },
        { id: "t-harden", owner: "kimi", reviewer: "claude", status: "in_progress", spec: "", evidence: "" },
      ],
    },
  ],
  awaiting_count: 1,
};

function makeSource(overrides: {
  cockpitPrompt?: DataSource["cockpitPrompt"];
  cockpitRespond?: DataSource["cockpitRespond"];
  cockpitCancel?: DataSource["cockpitCancel"];
  cockpitResume?: DataSource["cockpitResume"];
  cockpitStatus?: DataSource["cockpitStatus"];
  cockpitStreamUrl?: DataSource["cockpitStreamUrl"];
  cockpitSubscribe?: DataSource["cockpitSubscribe"];
} = {}): DataSource {
  return {
    capabilities: {
      canWrite: true,
      canOrchestrate: true,
      multiMachine: false,
      cockpit: true,
    },
    listProjects: vi.fn(),
    getState: vi.fn(),
    getStats: vi.fn(),
    subscribe: vi.fn(),
    cockpitPrompt: overrides.cockpitPrompt ?? vi.fn(),
    cockpitRespond: overrides.cockpitRespond ?? vi.fn(),
    cockpitCancel: overrides.cockpitCancel ?? vi.fn(),
    cockpitResume: overrides.cockpitResume ?? vi.fn(),
    cockpitStatus:
      overrides.cockpitStatus ??
      vi.fn().mockResolvedValue({ threadId: "", capable: true, pending: [] } as CockpitStatus),
    cockpitStreamUrl: overrides.cockpitStreamUrl ?? vi.fn().mockReturnValue("/fake-cockpit-stream"),
    cockpitSubscribe:
      overrides.cockpitSubscribe ??
      vi.fn((_project, _seat, cb) => {
        const sub = new FakeSub(cb);
        return () => sub.close();
      }),
  } as unknown as DataSource;
}

function renderPanel(
  source: DataSource,
  opts: {
    seat?: string;
    agents?: Seat[];
    state?: State;
    onClose?: () => void;
    onNotify?: (m: string, kind?: "error") => void;
    onSeatChange?: (s: string) => void;
    onOpenBoard?: (taskId?: string) => void;
  } = {},
) {
  return render(
    <DataSourceProvider source={source}>
      <CockpitPanel
        project="p1"
        seat={opts.seat ?? "claude"}
        agents={opts.agents ?? defaultAgents}
        state={opts.state ?? sampleState}
        onClose={opts.onClose ?? vi.fn()}
        onNotify={opts.onNotify}
        onSeatChange={opts.onSeatChange}
        onOpenBoard={opts.onOpenBoard}
        viewMode
      />
    </DataSourceProvider>,
  );
}

beforeEach(() => {
  lastSub = null;
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("CockpitPanel", () => {
  it("subscribes via cockpitSubscribe and unsubscribes on unmount", () => {
    const source = makeSource();
    const { unmount } = renderPanel(source);
    expect(source.cockpitSubscribe).toHaveBeenCalledWith("p1", "claude", expect.any(Function));
    expect(lastSub).not.toBeNull();
    expect(lastSub!.closed).toBe(false);
    unmount();
    expect(lastSub!.closed).toBe(true);
  });

  it("accumulates message events into an assistant bubble", async () => {
    renderPanel(makeSource());
    expect(lastSub).not.toBeNull();

    lastSub!.emit({ kind: "message", text: "Hello" });
    lastSub!.emit({ kind: "message", text: " world" });
    await waitFor(() => expect(screen.getByTestId("cockpit-message")).toHaveTextContent("Hello world"));
    expect(screen.getByTestId("cockpit-message").dataset.role).toBe("assistant");
  });

  it("renders tool-call cards from tool events", async () => {
    renderPanel(makeSource());
    expect(lastSub).not.toBeNull();

    lastSub!.emit({ kind: "tool", tool: { name: "grep", phase: "start" } });
    lastSub!.emit({ kind: "tool", tool: { name: "grep", phase: "end", text: "internal/relay/config.go:41\ninternal/relay/client.go:88" } });
    await waitFor(() => expect(screen.getByTestId("cockpit-tool-card")).toHaveTextContent("grep"));
    expect(screen.getByTestId("cockpit-tool-card")).toHaveTextContent("internal/relay/config.go:41");
  });

  it("renders system rows for errors", async () => {
    renderPanel(makeSource());
    expect(lastSub).not.toBeNull();

    lastSub!.emit({ kind: "error", err: "something broke" });
    await waitFor(() => expect(screen.getByTestId("cockpit-system-row")).toHaveTextContent("something broke"));
  });

  it("sends a prompt from the input and renders the user bubble", async () => {
    const cockpitPrompt = vi.fn().mockResolvedValue({ ok: true, threadId: "t1" });
    const source = makeSource({ cockpitPrompt });
    renderPanel(source);

    fireEvent.change(screen.getByTestId("cockpit-input"), { target: { value: "do the thing" } });
    fireEvent.click(screen.getByTestId("cockpit-send"));

    await waitFor(() => expect(cockpitPrompt).toHaveBeenCalledWith("p1", "claude", "do the thing"));
    expect(screen.getByTestId("cockpit-message").dataset.role).toBe("user");
  });

  it("renders pending approvals in stream and calls cockpitRespond with allow", async () => {
    const cockpitRespond = vi.fn().mockResolvedValue(undefined);
    const source = makeSource({
      cockpitStatus: vi.fn().mockResolvedValue({
        threadId: "t1",
        capable: true,
        pending: [{ id: "a1", kind: "tool", toolName: "read_file" }],
      } as CockpitStatus),
      cockpitRespond,
    });
    renderPanel(source);

    await waitFor(() => expect(screen.getByTestId("cockpit-approval")).toBeInTheDocument());
    expect(screen.getByTestId("cockpit-approval")).toHaveTextContent("read_file");

    fireEvent.click(screen.getByTestId("cockpit-approval-allow"));
    await waitFor(() => expect(cockpitRespond).toHaveBeenCalledWith("p1", "claude", "a1", "allow"));
  });

  it("shows exec risk badge and requires two-step Allow confirmation", async () => {
    const cockpitRespond = vi.fn().mockResolvedValue(undefined);
    const source = makeSource({
      cockpitStatus: vi.fn().mockResolvedValue({
        threadId: "t1",
        capable: true,
        pending: [{ id: "a1", kind: "tool", toolName: "bash", risk: "exec" }],
      } as CockpitStatus),
      cockpitRespond,
    });
    renderPanel(source);

    await waitFor(() => expect(screen.getByTestId("cockpit-approval")).toBeInTheDocument());
    const badge = screen.getByTestId("cockpit-approval-risk");
    expect(badge).toHaveTextContent("exec");
    expect(badge).toHaveStyle({ color: "var(--color-danger)" });

    fireEvent.click(screen.getByTestId("cockpit-approval-allow"));
    await waitFor(() =>
      expect(screen.getByTestId("cockpit-approval-allow-confirm")).toHaveTextContent("Confirm allow ▸"),
    );
    expect(cockpitRespond).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("cockpit-approval-allow-confirm"));
    await waitFor(() => expect(cockpitRespond).toHaveBeenCalledWith("p1", "claude", "a1", "allow"));
  });

  it("renders a resume banner when resumable and no live stream content", async () => {
    const cockpitResume = vi.fn().mockResolvedValue({ ok: true, threadId: "stored-thread" });
    const cockpitStatus = vi
      .fn()
      .mockResolvedValue({ threadId: "", capable: true, resumable: true, pending: [] } as CockpitStatus);
    const source = makeSource({ cockpitResume, cockpitStatus });
    renderPanel(source);

    await waitFor(() => expect(screen.getByTestId("cockpit-resume")).toBeInTheDocument());
    expect(screen.getByTestId("cockpit-resume")).toHaveTextContent("Previous session available — Resume");

    const prevSub = lastSub;
    fireEvent.click(screen.getByTestId("cockpit-resume-button"));
    await waitFor(() => expect(cockpitResume).toHaveBeenCalledWith("p1", "claude"));
    await waitFor(() => expect(source.cockpitSubscribe).toHaveBeenCalledTimes(2));
    expect(lastSub).not.toBe(prevSub);
  });

  it("hides the resume banner once stream content arrives", async () => {
    const source = makeSource();
    renderPanel(source);
    await waitFor(() => expect(source.cockpitSubscribe).toHaveBeenCalledTimes(1));

    lastSub!.emit({ kind: "message", text: "hi" });
    await waitFor(() => expect(screen.getByTestId("cockpit-message")).toHaveTextContent("hi"));
    expect(screen.queryByTestId("cockpit-resume")).not.toBeInTheDocument();
  });

  it("allows read-risk approvals with a single click", async () => {
    const cockpitRespond = vi.fn().mockResolvedValue(undefined);
    const source = makeSource({
      cockpitStatus: vi.fn().mockResolvedValue({
        threadId: "t1",
        capable: true,
        pending: [{ id: "a1", kind: "tool", toolName: "read_file", risk: "read" }],
      } as CockpitStatus),
      cockpitRespond,
    });
    renderPanel(source);

    await waitFor(() => expect(screen.getByTestId("cockpit-approval")).toBeInTheDocument());
    expect(screen.getByTestId("cockpit-approval-risk")).toHaveTextContent("read");

    fireEvent.click(screen.getByTestId("cockpit-approval-allow"));
    await waitFor(() => expect(cockpitRespond).toHaveBeenCalledWith("p1", "claude", "a1", "allow"));
  });

  it("calls onClose on Escape when not in view mode", () => {
    const onClose = vi.fn();
    render(
      <DataSourceProvider source={makeSource()}>
        <CockpitPanel project="p1" seat="claude" agents={defaultAgents} onClose={onClose} />
      </DataSourceProvider>,
    );
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalled();
  });

  it("renders rawInput in approval card and truncates long input", async () => {
    const longValue = "a".repeat(700);
    const source = makeSource({
      cockpitStatus: vi.fn().mockResolvedValue({
        threadId: "t1",
        capable: true,
        pending: [{ id: "a1", kind: "tool", toolName: "read_file", rawInput: { path: longValue } }],
      } as CockpitStatus),
    });
    renderPanel(source);

    await waitFor(() => expect(screen.getByTestId("cockpit-approval-rawinput")).toBeInTheDocument());
    const block = screen.getByTestId("cockpit-approval-rawinput");
    const expected = "$ " + JSON.stringify({ path: longValue }, null, 1).slice(0, 600) + "…";
    expect(block.textContent).toBe(expected);
  });

  it("does not render rawInput block when rawInput is absent", async () => {
    const source = makeSource({
      cockpitStatus: vi.fn().mockResolvedValue({
        threadId: "t1",
        capable: true,
        pending: [{ id: "a1", kind: "tool", toolName: "read_file" }],
      } as CockpitStatus),
    });
    renderPanel(source);

    await waitFor(() => expect(screen.getByTestId("cockpit-approval")).toBeInTheDocument());
    expect(screen.queryByTestId("cockpit-approval-rawinput")).not.toBeInTheDocument();
  });

  it("disables input and shows a friendly reason when the seat is not capable", async () => {
    const reason = 'seat "claude" has no deep-integration or ACP kind (kind="")';
    const source = makeSource({
      cockpitStatus: vi.fn().mockResolvedValue({
        threadId: "",
        capable: false,
        reason,
        pending: [],
      } as CockpitStatus),
    });
    renderPanel(source);

    await waitFor(() => expect(screen.getByTestId("cockpit-notice")).toHaveTextContent(reason));
    const input = screen.getByTestId("cockpit-input");
    expect(input).toBeDisabled();
    expect(input).toHaveAttribute("placeholder", "This seat can't host a cockpit");
    expect(screen.getByTestId("cockpit-send")).toBeDisabled();
  });

  it("shows streaming indicator while a tool is running", async () => {
    renderPanel(makeSource());
    expect(lastSub).not.toBeNull();

    lastSub!.emit({ kind: "tool", tool: { name: "read_file", phase: "start" } });
    await waitFor(() => expect(screen.getByTestId("cockpit-streaming")).toBeInTheDocument());

    lastSub!.emit({ kind: "state", state: "turn_completed" });
    await waitFor(() => expect(screen.queryByTestId("cockpit-streaming")).not.toBeInTheDocument());
  });

  it("displays the threadId in the session info card", async () => {
    const source = makeSource({
      cockpitStatus: vi.fn().mockResolvedValue({
        threadId: "thread-abc-123",
        capable: true,
        pending: [],
      } as CockpitStatus),
    });
    renderPanel(source);

    await waitFor(() => expect(screen.getByTestId("cockpit-session-info")).toHaveTextContent("thread-abc"));
  });

  it("auto-scrolls to the bottom when the user is near the bottom", async () => {
    renderPanel(makeSource());
    const messages = screen.getByTestId("cockpit-messages");

    let scrollTop = 80;
    Object.defineProperty(messages, "scrollHeight", { configurable: true, get: () => 200 });
    Object.defineProperty(messages, "clientHeight", { configurable: true, get: () => 100 });
    Object.defineProperty(messages, "scrollTop", {
      configurable: true,
      get: () => scrollTop,
      set: (v: number) => {
        scrollTop = v;
      },
    });

    lastSub!.emit({ kind: "message", text: "hi" });
    await waitFor(() => expect(scrollTop).toBe(200));
  });

  it("does not auto-scroll when the user has scrolled up", async () => {
    renderPanel(makeSource());
    const messages = screen.getByTestId("cockpit-messages");

    let scrollTop = 10;
    Object.defineProperty(messages, "scrollHeight", { configurable: true, get: () => 200 });
    Object.defineProperty(messages, "clientHeight", { configurable: true, get: () => 100 });
    Object.defineProperty(messages, "scrollTop", {
      configurable: true,
      get: () => scrollTop,
      set: (v: number) => {
        scrollTop = v;
      },
    });

    lastSub!.emit({ kind: "message", text: "hi" });
    await waitFor(() => expect(screen.getByTestId("cockpit-message")).toHaveTextContent("hi"));
    expect(scrollTop).toBe(10);
  });

  it("switches seat from the sessions list", async () => {
    const source = makeSource();
    const onSeatChange = vi.fn();
    renderPanel(source, { agents: defaultAgents, onSeatChange });

    await waitFor(() => expect(screen.getByTestId("cockpit-sessions")).toBeInTheDocument());
    const kimiRow = screen.getByText(/kimi/);
    fireEvent.click(kimiRow);
    expect(onSeatChange).toHaveBeenCalledWith("kimi");
  });

  it("notifies on rate-limit errors", async () => {
    const onNotify = vi.fn();
    const cockpitPrompt = vi.fn().mockRejectedValue(new Error("429 Too Many Requests"));
    const source = makeSource({ cockpitPrompt });
    renderPanel(source, { onNotify });

    fireEvent.change(screen.getByTestId("cockpit-input"), { target: { value: "go" } });
    fireEvent.click(screen.getByTestId("cockpit-send"));

    await waitFor(() => expect(screen.getByTestId("cockpit-error")).toHaveTextContent("Rate limited"));
    expect(onNotify).toHaveBeenCalledWith("Rate limited (429)", "error");
  });

  it("renders the approval queue count pill", async () => {
    const source = makeSource({
      cockpitStatus: vi.fn().mockResolvedValue({
        threadId: "t1",
        capable: true,
        pending: [
          { id: "a1", kind: "tool", toolName: "bash", risk: "exec" },
          { id: "a2", kind: "tool", toolName: "write_file" },
        ],
      } as CockpitStatus),
    });
    renderPanel(source);

    await waitFor(() => expect(screen.getByTestId("cockpit-approval-queue")).toHaveTextContent("2 pending"));
    expect(screen.getAllByTestId("cockpit-queue-approval")).toHaveLength(2);
  });

  it("renders run context mini-pipeline from state", async () => {
    renderPanel(makeSource());
    await waitFor(() => expect(screen.getByTestId("cockpit-run-context")).toBeInTheDocument());
    const ctx = screen.getByTestId("cockpit-run-context");
    expect(ctx).toHaveTextContent("feat-rh");
    expect(ctx).toHaveTextContent("t-cfg");
    expect(ctx).toHaveTextContent("t-impl");
    expect(ctx).toHaveTextContent("t-harden");
  });

  it("navigates to board when run context is clicked", async () => {
    const onOpenBoard = vi.fn();
    renderPanel(makeSource(), { onOpenBoard });
    await waitFor(() => expect(screen.getByTestId("cockpit-run-context")).toBeInTheDocument());
    fireEvent.click(screen.getByTestId("cockpit-run-context"));
    expect(onOpenBoard).toHaveBeenCalled();
  });
});
