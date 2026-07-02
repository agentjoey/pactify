import { render, screen, act } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

let emit: (line: string, lastEventId?: string) => void = () => {};
vi.mock("../../lib/api", () => ({
  subscribeAgentStream: (_p: string, _t: string, onLine: (l: string, id?: string) => void) => {
    emit = onLine;
    return () => {};
  },
}));

import { AgentTerminal } from "./AgentTerminal";

describe("AgentTerminal", () => {
  it("renders streamed lines", async () => {
    render(<AgentTerminal project="p" task="t1" seat="opencode" />);
    await act(async () => { emit("$ opencode run t1"); emit("✓ PASS"); });
    expect(screen.getByText("$ opencode run t1")).toBeTruthy();
    expect(screen.getByText("✓ PASS")).toBeTruthy();
  });

  it("ignores a duplicate backfill batch after reconnect (id ordinals)", async () => {
    render(<AgentTerminal project="p" task="t1" />);
    await act(async () => { emit("alpha", "1"); emit("beta", "2"); });
    // EventSource reconnect replays the backfill: same lines, same ordinals.
    await act(async () => { emit("alpha", "1"); emit("beta", "2"); emit("gamma", "3"); });
    const terminal = screen.getByTestId("agent-terminal");
    expect(screen.getAllByText("alpha")).toHaveLength(1);
    expect(screen.getAllByText("beta")).toHaveLength(1);
    expect(screen.getByText("gamma")).toBeTruthy();
    // 3 unique lines + header + waiting placeholder absent
    expect(terminal.textContent).not.toContain("waiting for agent output");
  });

  it("still appends id-less lines unconditionally (legacy server)", async () => {
    render(<AgentTerminal project="p" task="t1" />);
    await act(async () => { emit("raw"); emit("raw"); });
    expect(screen.getAllByText("raw")).toHaveLength(2);
  });
});
