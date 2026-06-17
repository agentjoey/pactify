import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const createManifest = vi.fn();
vi.mock("../../lib/api", () => ({ createManifest: (...a: unknown[]) => createManifest(...a) }));

import { CustomAgentForm } from "./CustomAgentForm";

describe("CustomAgentForm", () => {
  beforeEach(() => {
    createManifest.mockReset();
  });

  it("posts the assembled TOML on submit", async () => {
    createManifest.mockResolvedValue({ kind: "myx" });
    render(<CustomAgentForm onCreated={() => {}} />);
    fireEvent.change(screen.getByTestId("ca-kind"), { target: { value: "myx" } });
    fireEvent.change(screen.getByTestId("ca-binary"), { target: { value: "myx" } });
    fireEvent.change(screen.getByTestId("ca-args"), { target: { value: "run,{briefing}" } });
    fireEvent.click(screen.getByRole("button", { name: /add custom agent/i }));
    await waitFor(() => expect(createManifest).toHaveBeenCalled());
    const toml = createManifest.mock.calls[0][0] as string;
    expect(toml).toContain('kind = "myx"');
    expect(toml).toContain('args = ["run", "{briefing}"]');
  });

  it("shows the server's field error", async () => {
    createManifest.mockRejectedValue(new Error("runner.args: must contain exactly one {briefing}"));
    render(<CustomAgentForm onCreated={() => {}} />);
    fireEvent.change(screen.getByTestId("ca-kind"), { target: { value: "myx" } });
    fireEvent.change(screen.getByTestId("ca-binary"), { target: { value: "myx" } });
    fireEvent.click(screen.getByRole("button", { name: /add custom agent/i }));
    await waitFor(() => expect(screen.getByText(/must contain exactly one/)).toBeTruthy());
  });
});
