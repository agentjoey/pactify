import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { WiringStatus, WireResult } from "../../lib/types";

const getWiring = vi.fn();
const postWire = vi.fn();

vi.mock("../../lib/api", () => ({
  getWiring: (...a: unknown[]) => getWiring(...a),
  postWire: (...a: unknown[]) => postWire(...a),
}));

import { Wiring } from "./Wiring";

const fixture: WiringStatus[] = [
  { kind: "opencode", wired: true, detail: "config opencode.json", path: "opencode.json", global: false, docOnly: false },
  { kind: "claude-desktop", wired: false, detail: "not wired", path: "~/Library/Application Support/Claude/claude_desktop_config.json", global: true, docOnly: false },
  { kind: "codex", wired: false, detail: "not wired", path: "~/.codex/config.toml", global: false, docOnly: true },
];

describe("Wiring", () => {
  beforeEach(() => {
    getWiring.mockReset().mockResolvedValue(fixture);
    postWire.mockReset();
  });

  it("renders a row per kind with status dot and machine-global tag", async () => {
    render(<Wiring project="demo" author={false} />);
    await waitFor(() => expect(screen.getByTestId("wiring-row-opencode")).toBeInTheDocument());

    expect(screen.getByTestId("wire-dot-opencode")).toHaveClass("bg-[#3fb950]");
    expect(screen.getByTestId("wire-dot-claude-desktop")).toHaveClass("bg-[#484f58]");
    // machine-global tag only on the global (desktop) kind.
    expect(screen.getByText("machine-global")).toBeInTheDocument();
  });

  it("hides Wire buttons in observe-only mode", async () => {
    render(<Wiring project="demo" author={false} />);
    await waitFor(() => expect(screen.getByTestId("wiring-row-opencode")).toBeInTheDocument());
    expect(screen.queryByText("Wire")).toBeNull();
    expect(screen.queryByText("show snippet")).toBeNull();
  });

  it("global kind requires the machine-global checkbox before Confirm", async () => {
    render(<Wiring project="demo" author={true} />);
    await waitFor(() => expect(screen.getByTestId("wiring-row-claude-desktop")).toBeInTheDocument());

    // open the modal for the global kind
    const row = screen.getByTestId("wiring-row-claude-desktop");
    fireEvent.click(row.querySelector("button")!);

    await waitFor(() => expect(screen.getByTestId("wire-modal-claude-desktop")).toBeInTheDocument());
    expect(screen.getByTestId("global-warn")).toBeInTheDocument();
    // the consent line names the exact machine-global path (from row.path).
    expect(screen.getByTestId("global-warn")).toHaveTextContent(
      "~/Library/Application Support/Claude/claude_desktop_config.json",
    );

    // fill valid seat + roles; Confirm still disabled until the checkbox.
    fireEvent.change(screen.getByLabelText("seat"), { target: { value: "claude" } });
    fireEvent.change(screen.getByLabelText("roles"), { target: { value: "worker" } });
    const confirm = screen.getByText("Confirm") as HTMLButtonElement;
    expect(confirm).toBeDisabled();

    fireEvent.click(screen.getByRole("checkbox"));
    expect(confirm).not.toBeDisabled();
  });

  it("docOnly kind shows the copyable snippet returned by the server", async () => {
    const res: WireResult = {
      wrote: true, global: false, path: ".pact/agents/codex.md", docOnly: true,
      snippet: "[mcp_servers.pact]\ncommand = \"pactify\"",
    };
    postWire.mockResolvedValue(res);

    render(<Wiring project="demo" author={true} />);
    await waitFor(() => expect(screen.getByTestId("wiring-row-codex")).toBeInTheDocument());

    fireEvent.click(screen.getByText("show snippet"));
    await waitFor(() => expect(screen.getByText("wire entry + show snippet")).toBeInTheDocument());

    fireEvent.change(screen.getByLabelText("seat"), { target: { value: "codex" } });
    fireEvent.change(screen.getByLabelText("roles"), { target: { value: "worker" } });
    fireEvent.click(screen.getByText("wire entry + show snippet"));

    await waitFor(() => {
      expect(screen.getByTestId("wire-snippet")).toHaveTextContent("[mcp_servers.pact]");
    });
    expect(postWire).toHaveBeenCalledWith("demo", "codex", { seat: "codex", roles: "worker" });
  });
});
