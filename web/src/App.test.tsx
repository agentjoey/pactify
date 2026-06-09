import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import App from "./App";

beforeEach(() => {
  // @ts-expect-error minimal EventSource stub
  globalThis.EventSource = class { close() {} addEventListener() {} };
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url === "/api/projects") return { ok: true, json: async () => [{ id: "demo", name: "demo", path: "/x", project: "demo", feature_count: 1, awaiting_count: 0 }] };
    return { ok: true, json: async () => ({ project: "demo", agents: [{ id: "claude-opus", roles: ["orchestrator"] }], features: [], awaiting_count: 0 }) };
  }));
});

describe("App", () => {
  it("loads projects and shows the switcher + an agent", async () => {
    render(<App />);
    expect(screen.getByTestId("app-root")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("option", { name: "demo" })).toBeInTheDocument());
    await waitFor(() => expect(screen.getByText(/claude-opus/)).toBeInTheDocument());
  });
});
