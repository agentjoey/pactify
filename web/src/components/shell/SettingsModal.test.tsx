import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { SettingsModal } from "./SettingsModal";

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.includes("/api/agents")) return new Response("[]", { status: 200 });
    if (url.includes("/wiring")) return new Response("[]", { status: 200 });
    if (url.includes("/seats")) return new Response("[]", { status: 200 });
    if (url.includes("/api/registry")) return new Response("[]", { status: 200 });
    return new Response("[]", { status: 200 });
  }));
});

describe("SettingsModal", () => {
  it("renders a dialog with machine-level Agents and project sections", () => {
    render(<SettingsModal project="demo" author={true} onClose={() => {}} />);
    expect(screen.getByTestId("settings-modal")).toBeInTheDocument();
    expect(screen.getAllByText(/Agents/i).length).toBeGreaterThan(0);
  });

  it("closes on the modal close button", () => {
    const onClose = vi.fn();
    render(<SettingsModal project="demo" author={true} onClose={onClose} />);
    fireEvent.click(screen.getByLabelText("close"));
    expect(onClose).toHaveBeenCalled();
  });
});
