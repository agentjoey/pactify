import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { CockpitPanel } from "./CockpitPanel";
import { DataSourceProvider } from "../lib/datasource";
import type { DataSource } from "../lib/datasource";
import type { CockpitStatus } from "../lib/api";

let lastES: FakeES | null = null;

class FakeES {
  onmessage: ((ev: MessageEvent) => void) | null = null;
  closed = false;
  url: string;
  constructor(url: string) {
    this.url = url;
    lastES = this;
  }
  close() {
    this.closed = true;
  }
}

function makeSource(overrides: {
  cockpitPrompt?: DataSource["cockpitPrompt"];
  cockpitRespond?: DataSource["cockpitRespond"];
  cockpitCancel?: DataSource["cockpitCancel"];
  cockpitStatus?: DataSource["cockpitStatus"];
  cockpitStreamUrl?: DataSource["cockpitStreamUrl"];
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
    cockpitStatus:
      overrides.cockpitStatus ??
      vi.fn().mockResolvedValue({ threadId: "", capable: true, pending: [] } as CockpitStatus),
    cockpitStreamUrl: overrides.cockpitStreamUrl ?? vi.fn().mockReturnValue("/fake-cockpit-stream"),
  } as unknown as DataSource;
}

function renderPanel(source: DataSource, onClose = vi.fn()) {
  return render(
    <DataSourceProvider source={source}>
      <CockpitPanel project="p1" seat="claude" onClose={onClose} />
    </DataSourceProvider>,
  );
}

beforeEach(() => {
  lastES = null;
  vi.stubGlobal("EventSource", FakeES);
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("CockpitPanel", () => {
  it("opens an EventSource and closes it on unmount", () => {
    const source = makeSource();
    const { unmount } = renderPanel(source);
    expect(lastES).not.toBeNull();
    expect(lastES!.url).toBe("/fake-cockpit-stream");
    const messages = screen.getByTestId("cockpit-messages");
    expect(messages).toHaveAttribute("role", "log");
    expect(messages).toHaveAttribute("aria-label", "Conversation");
    unmount();
    expect(lastES!.closed).toBe(true);
  });

  it("accumulates message events into an assistant bubble and renders system rows", async () => {
    renderPanel(makeSource());
    expect(lastES).not.toBeNull();

    lastES!.onmessage?.({ data: JSON.stringify({ kind: "message", text: "Hello" }) } as MessageEvent);
    lastES!.onmessage?.({ data: JSON.stringify({ kind: "message", text: " world" }) } as MessageEvent);
    await waitFor(() => expect(screen.getByTestId("cockpit-message")).toHaveTextContent("Hello world"));
    expect(screen.getByTestId("cockpit-message").dataset.role).toBe("assistant");

    lastES!.onmessage?.({
      data: JSON.stringify({ kind: "tool", tool: { name: "read_file", phase: "call", text: "foo.txt" } }),
    } as MessageEvent);
    lastES!.onmessage?.({
      data: JSON.stringify({ kind: "error", err: "something broke" }),
    } as MessageEvent);
    await waitFor(() => expect(screen.getAllByTestId("cockpit-system-row")).toHaveLength(2));
    const rows = screen.getAllByTestId("cockpit-system-row");
    expect(rows[0]).toHaveTextContent("read_file (call): foo.txt");
    expect(rows[1]).toHaveTextContent("something broke");
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

  it("renders pending approvals and calls cockpitRespond with allow", async () => {
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
    expect(screen.getByTestId("cockpit-approval-tool")).toHaveTextContent("read_file · tool");

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

  it("calls onClose when the close button is clicked", () => {
    const onClose = vi.fn();
    renderPanel(makeSource(), onClose);
    fireEvent.click(screen.getByTestId("cockpit-close"));
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
    const expected = JSON.stringify({ path: longValue }, null, 1).slice(0, 600) + "…";
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

  it("shows and clears the running-tool indicator", async () => {
    renderPanel(makeSource());
    expect(lastES).not.toBeNull();

    lastES!.onmessage?.({
      data: JSON.stringify({ kind: "tool", tool: { name: "read_file", phase: "start" } }),
    } as MessageEvent);
    await waitFor(() =>
      expect(screen.getByTestId("cockpit-running")).toHaveTextContent("⏺ read_file running…"),
    );

    lastES!.onmessage?.({
      data: JSON.stringify({ kind: "state", state: "turn_completed" }),
    } as MessageEvent);
    await waitFor(() => expect(screen.queryByTestId("cockpit-running")).not.toBeInTheDocument());
  });

  it("displays the threadId in the header", async () => {
    const source = makeSource({
      cockpitStatus: vi.fn().mockResolvedValue({
        threadId: "thread-abc-123",
        capable: true,
        pending: [],
      } as CockpitStatus),
    });
    renderPanel(source);

    await waitFor(() => expect(screen.getByTestId("cockpit-thread-id")).toHaveTextContent("thread-a"));
    expect(screen.getByTestId("cockpit-thread-id")).toHaveAttribute("title", "thread-abc-123");
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

    lastES!.onmessage?.({ data: JSON.stringify({ kind: "message", text: "hi" }) } as MessageEvent);
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

    lastES!.onmessage?.({ data: JSON.stringify({ kind: "message", text: "hi" }) } as MessageEvent);
    await waitFor(() => expect(screen.getByTestId("cockpit-message")).toHaveTextContent("hi"));
    expect(scrollTop).toBe(10);
  });
});
