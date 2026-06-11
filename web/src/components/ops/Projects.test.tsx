import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { RegistryEntry } from "../../lib/types";

const getRegistry = vi.fn();
const postRegister = vi.fn();
const deleteRegistry = vi.fn();

vi.mock("../../lib/api", () => ({
  getRegistry: (...a: unknown[]) => getRegistry(...a),
  postRegister: (...a: unknown[]) => postRegister(...a),
  deleteRegistry: (...a: unknown[]) => deleteRegistry(...a),
}));

import { Projects } from "./Projects";

const fixture: RegistryEntry[] = [
  { name: "demo", path: "/repos/demo", status: { valid: true, seats: 2, lastEventTs: "2026-06-11T11:59:30Z" } },
  { name: "broken", path: "/repos/broken", status: { valid: false, error: "missing .pact dir", seats: 0 } },
];

describe("Projects", () => {
  beforeEach(() => {
    getRegistry.mockReset().mockResolvedValue(fixture);
    postRegister.mockReset().mockResolvedValue({ name: "x" });
    deleteRegistry.mockReset().mockResolvedValue(undefined);
  });

  it("renders registry cards with valid/error badges and counts", async () => {
    render(<Projects author={false} />);
    await waitFor(() => expect(screen.getByTestId("project-card-demo")).toBeInTheDocument());

    expect(screen.getByTestId("project-badge-demo")).toHaveTextContent("✓");
    expect(screen.getByText("2 seats")).toBeInTheDocument();

    expect(screen.getByTestId("project-badge-broken")).toHaveTextContent("✗");
    expect(screen.getByText("missing .pact dir")).toBeInTheDocument();
  });

  it("hides the register form and Remove buttons in observe-only mode", async () => {
    render(<Projects author={false} />);
    await waitFor(() => expect(screen.getByTestId("project-card-demo")).toBeInTheDocument());
    expect(screen.queryByTestId("register-form")).toBeNull();
    expect(screen.queryByTestId("project-remove-demo")).toBeNull();
  });

  it("shows the server error verbatim when register fails", async () => {
    postRegister.mockRejectedValue(new Error("path is not a git repo"));
    render(<Projects author={true} />);
    await waitFor(() => expect(screen.getByTestId("register-form")).toBeInTheDocument());

    fireEvent.change(screen.getByLabelText("path"), { target: { value: "/tmp/not-a-repo" } });
    fireEvent.click(screen.getByText("Register"));

    await waitFor(() => {
      expect(screen.getByTestId("register-error")).toHaveTextContent("path is not a git repo");
    });
  });

  it("calls onChanged and re-fetches after a successful register", async () => {
    const onChanged = vi.fn();
    render(<Projects author={true} onChanged={onChanged} />);
    await waitFor(() => expect(screen.getByTestId("register-form")).toBeInTheDocument());

    fireEvent.change(screen.getByLabelText("path"), { target: { value: "/repos/new" } });
    fireEvent.click(screen.getByText("Register"));

    await waitFor(() => {
      expect(postRegister).toHaveBeenCalledWith("/repos/new", undefined);
      expect(onChanged).toHaveBeenCalled();
    });
  });
});
