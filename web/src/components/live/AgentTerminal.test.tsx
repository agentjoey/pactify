import { render, screen, act } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

let emit: (line: string) => void = () => {};
vi.mock("../../lib/api", () => ({
  subscribeAgentStream: (_p: string, _t: string, onLine: (l: string) => void) => {
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
});
