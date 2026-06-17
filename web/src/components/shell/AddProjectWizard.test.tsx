import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";

const browseFs = vi.fn();
const getSetupSuggest = vi.fn();
const postRegister = vi.fn();
const setupApply = vi.fn();

vi.mock("../../lib/api", () => ({
  browseFs: (...args: unknown[]) => browseFs(...args),
  getSetupSuggest: (...args: unknown[]) => getSetupSuggest(...args),
  postRegister: (...args: unknown[]) => postRegister(...args),
  setupApply: (...args: unknown[]) => setupApply(...args),
}));

import { AddProjectWizard } from "./AddProjectWizard";

const ENTRY_NEW = { name: "new", path: "/home/new", isGit: true, hasPact: false };
const ENTRY_EXISTING = { name: "existing", path: "/home/existing", isGit: true, hasPact: true };

describe("AddProjectWizard", () => {
  beforeEach(() => {
    browseFs.mockReset();
    getSetupSuggest.mockReset();
    postRegister.mockReset();
    setupApply.mockReset();
    browseFs.mockResolvedValue({ path: "/home", parent: "/", entries: [ENTRY_NEW, ENTRY_EXISTING] });
    getSetupSuggest.mockResolvedValue({
      bindings: [
        { seat: "claude", kind: "claude-code", roles: ["orchestrator", "reviewer"], drivable: true },
        { seat: "opencode", kind: "opencode", roles: ["worker"], drivable: true },
      ],
      warnings: [],
    });
  });

  it("opens the modal when open is true", () => {
    render(<AddProjectWizard open onClose={() => {}} onAdded={() => {}} />);
    expect(screen.getByTestId("add-project-wizard")).toBeTruthy();
    expect(screen.getByTestId("folder-picker")).toBeTruthy();
  });

  it("advances to step 2 with a roster for new projects", async () => {
    render(<AddProjectWizard open onClose={() => {}} onAdded={() => {}} />);
    await waitFor(() => expect(screen.getByTestId("folder-entry-new-checkbox")).toBeTruthy());
    fireEvent.click(screen.getByTestId("folder-entry-new-checkbox"));
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
    await waitFor(() => expect(screen.getByTestId("wizard-step2")).toBeTruthy());
    expect(screen.getByTestId("wizard-folder-new")).toHaveTextContent("new project");
    expect(screen.getByTestId("role-claude-orchestrator")).toBeTruthy();
  });

  it("registers existing pactify projects and initializes new ones", async () => {
    postRegister.mockResolvedValue({ name: "existing" });
    setupApply.mockResolvedValue({ inited: true, wired: [], notes: [] });
    const onAdded = vi.fn();
    render(<AddProjectWizard open onClose={() => {}} onAdded={onAdded} />);
    await waitFor(() => expect(screen.getByTestId("folder-entry-new-checkbox")).toBeTruthy());
    fireEvent.click(screen.getByTestId("folder-entry-new-checkbox"));
    fireEvent.click(screen.getByTestId("folder-entry-existing-checkbox"));
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
    await waitFor(() => expect(screen.getByTestId("wizard-step2")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: /Submit/i }));
    await waitFor(() => expect(postRegister).toHaveBeenCalledWith("/home/existing", { group: undefined }));
    await waitFor(() => expect(setupApply).toHaveBeenCalledWith(expect.objectContaining({ path: "/home/new", project: "new", group: undefined })));
    fireEvent.click(screen.getByRole("button", { name: /Done/i }));
    await waitFor(() => expect(onAdded).toHaveBeenCalled());
  });

  it("sends the group to both register and setup-apply", async () => {
    postRegister.mockResolvedValue({ name: "existing" });
    setupApply.mockResolvedValue({ inited: true, wired: [], notes: [] });
    render(<AddProjectWizard open onClose={() => {}} onAdded={() => {}} />);
    await waitFor(() => expect(screen.getByTestId("folder-entry-new-checkbox")).toBeTruthy());
    fireEvent.change(screen.getByTestId("wizard-group"), { target: { value: "team" } });
    fireEvent.click(screen.getByTestId("folder-entry-new-checkbox"));
    fireEvent.click(screen.getByTestId("folder-entry-existing-checkbox"));
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
    await waitFor(() => expect(screen.getByTestId("wizard-step2")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: /Submit/i }));
    await waitFor(() => expect(postRegister).toHaveBeenCalledWith("/home/existing", { group: "team" }));
    await waitFor(() => expect(setupApply).toHaveBeenCalledWith(expect.objectContaining({ group: "team" })));
  });

  it("shows doc-only snippets in results", async () => {
    setupApply.mockResolvedValue({
      inited: true,
      wired: [{ kind: "codex-cli", seat: "codex", wrote: false, path: "codex.toml", docOnly: true, snippet: "[agent]\nseat = codex" }],
      notes: [],
    });
    render(<AddProjectWizard open onClose={() => {}} onAdded={() => {}} />);
    await waitFor(() => expect(screen.getByTestId("folder-entry-new-checkbox")).toBeTruthy());
    fireEvent.click(screen.getByTestId("folder-entry-new-checkbox"));
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
    await waitFor(() => expect(screen.getByTestId("wizard-step2")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: /Submit/i }));
    await waitFor(() => expect(screen.getByTestId("wizard-results")).toBeTruthy());
    expect(screen.getByText(/doc-only/)).toBeTruthy();
    expect(screen.getByText(/seat = codex/)).toBeTruthy();
  });

  it("keeps the wizard open and shows failures without calling onAdded", async () => {
    postRegister.mockRejectedValue(new Error("already registered"));
    setupApply.mockResolvedValue({ inited: true, wired: [], notes: [] });
    const onAdded = vi.fn();
    render(<AddProjectWizard open onClose={() => {}} onAdded={onAdded} />);
    await waitFor(() => expect(screen.getByTestId("folder-entry-new-checkbox")).toBeTruthy());
    fireEvent.click(screen.getByTestId("folder-entry-new-checkbox"));
    fireEvent.click(screen.getByTestId("folder-entry-existing-checkbox"));
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
    await waitFor(() => expect(screen.getByTestId("wizard-step2")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: /Submit/i }));
    await waitFor(() => expect(screen.getByTestId("wizard-results")).toBeTruthy());
    expect(screen.queryByRole("button", { name: /Done/i })).toBeNull();
    expect(onAdded).not.toHaveBeenCalled();
  });

  it("excludes agents with no roles (marks them 'not added', doesn't send them)", async () => {
    getSetupSuggest.mockResolvedValue({
      bindings: [
        { seat: "claude", kind: "claude-code", roles: ["orchestrator", "reviewer", "worker"], drivable: true },
        { seat: "opencode", kind: "opencode", roles: ["worker"], drivable: true },
        { seat: "gemini", kind: "gemini-cli", roles: ["worker"], drivable: true },
      ],
      warnings: [],
    });
    setupApply.mockResolvedValue({ inited: true, wired: [], notes: [] });
    render(<AddProjectWizard open onClose={() => {}} onAdded={() => {}} />);
    await waitFor(() => expect(screen.getByTestId("folder-entry-new-checkbox")).toBeTruthy());
    fireEvent.click(screen.getByTestId("folder-entry-new-checkbox"));
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
    await waitFor(() => expect(screen.getByTestId("wizard-step2")).toBeTruthy());
    // Remove gemini's only role → it's "not added", but claude still covers o+r+w.
    fireEvent.click(screen.getByTestId("role-gemini-worker"));
    expect(screen.getByTestId("not-added-gemini")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Submit/i }));
    await waitFor(() => expect(setupApply).toHaveBeenCalled());
    const seats = setupApply.mock.calls[0][0].seats as { id: string }[];
    expect(seats.map((s) => s.id).sort()).toEqual(["claude", "opencode"]);
    expect(seats.find((s) => s.id === "gemini")).toBeUndefined();
  });

  it("blocks submit when the joined roster misses a required role", async () => {
    getSetupSuggest.mockResolvedValue({
      bindings: [{ seat: "opencode", kind: "opencode", roles: ["worker"], drivable: true }],
      warnings: [],
    });
    render(<AddProjectWizard open onClose={() => {}} onAdded={() => {}} />);
    await waitFor(() => expect(screen.getByTestId("folder-entry-new-checkbox")).toBeTruthy());
    fireEvent.click(screen.getByTestId("folder-entry-new-checkbox"));
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
    await waitFor(() => expect(screen.getByTestId("wizard-step2")).toBeTruthy());
    // Only opencode(worker) → missing orchestrator + reviewer → Submit disabled.
    expect(screen.getByRole("button", { name: /Submit/i })).toBeDisabled();
  });
});
