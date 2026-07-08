import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { RelayConnect } from "./RelayConnect";
import * as source from "../lib/source";
import type { RelaySource } from "../lib/relaysource";

vi.mock("../lib/source", async (orig) => {
  const actual = await orig<typeof import("../lib/source")>();
  return { ...actual, connectRelaySource: vi.fn(), relayUrl: () => "https://relay.test" };
});

describe("RelayConnect", () => {
  beforeEach(() => vi.clearAllMocks());

  it("connects and reports the source on success", async () => {
    const fakeSource = { capabilities: { canWrite: false, canOrchestrate: true, multiMachine: true, cockpit: false } } as unknown as RelaySource;
    (source.connectRelaySource as ReturnType<typeof vi.fn>).mockResolvedValue(fakeSource);
    const onConnected = vi.fn();
    render(<RelayConnect onConnected={onConnected} />);

    fireEvent.change(screen.getByLabelText(/master secret/i), { target: { value: "00ff" } });
    fireEvent.click(screen.getByRole("button", { name: /connect/i }));

    await waitFor(() => expect(onConnected).toHaveBeenCalledWith(fakeSource));
    expect(source.connectRelaySource).toHaveBeenCalledWith("00ff");
  });

  it("shows the error and does not connect on failure", async () => {
    (source.connectRelaySource as ReturnType<typeof vi.fn>).mockRejectedValue(new Error("auth failed: 401"));
    const onConnected = vi.fn();
    render(<RelayConnect onConnected={onConnected} />);

    fireEvent.change(screen.getByLabelText(/master secret/i), { target: { value: "00ff" } });
    fireEvent.click(screen.getByRole("button", { name: /connect/i }));

    await waitFor(() => expect(screen.getByText(/auth failed: 401/)).toBeInTheDocument());
    expect(onConnected).not.toHaveBeenCalled();
  });

  it("disables Connect until a secret is entered", () => {
    render(<RelayConnect onConnected={vi.fn()} />);
    expect(screen.getByRole("button", { name: /connect/i })).toBeDisabled();
  });
});
