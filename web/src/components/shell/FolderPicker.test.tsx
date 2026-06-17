import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { useState } from "react";

const browseFs = vi.fn();
vi.mock("../../lib/api", () => ({
  browseFs: (...args: unknown[]) => browseFs(...args),
}));

import { FolderPicker } from "./FolderPicker";

function StatefulPicker() {
  const [selected, setSelected] = useState<string[]>([]);
  return <FolderPicker selected={selected} onChange={(s) => { setSelected(s); }} />;
}

const HOME = {
  path: "/home",
  parent: "/",
  entries: [
    { name: "repo", path: "/home/repo", isGit: true, hasPact: true },
    { name: "new", path: "/home/new", isGit: true, hasPact: false },
  ],
};

const REPO = {
  path: "/home/repo",
  parent: "/home",
  entries: [{ name: "src", path: "/home/repo/src", isGit: false, hasPact: false }],
};

describe("FolderPicker", () => {
  beforeEach(() => {
    browseFs.mockReset();
    browseFs.mockResolvedValue(HOME);
  });

  it("loads the home directory on mount", async () => {
    render(<FolderPicker selected={[]} onChange={() => {}} />);
    await waitFor(() => expect(screen.getByTestId("folder-picker-path")).toHaveTextContent("/home"));
    expect(screen.getByTestId("folder-entry-repo")).toBeTruthy();
    expect(screen.getByTestId("folder-entry-new")).toBeTruthy();
  });

  it("marks hasPact entries and git entries", async () => {
    render(<FolderPicker selected={[]} onChange={() => {}} />);
    await waitFor(() => expect(screen.getByTestId("folder-entry-repo-pact")).toHaveTextContent("pactify project"));
    expect(screen.getAllByText("git")).toHaveLength(2);
  });

  it("toggles selection via checkbox", async () => {
    const onChange = vi.fn();
    render(<FolderPicker selected={[]} onChange={onChange} />);
    await waitFor(() => expect(screen.getByTestId("folder-entry-new-checkbox")).toBeTruthy());
    fireEvent.click(screen.getByTestId("folder-entry-new-checkbox"));
    await waitFor(() => expect(onChange).toHaveBeenCalledWith(["/home/new"], [HOME.entries[1]]));
  });

  it("navigates into a folder and back", async () => {
    browseFs.mockResolvedValueOnce(HOME).mockResolvedValueOnce(REPO);
    render(<StatefulPicker />);
    await waitFor(() => expect(screen.getByTestId("folder-entry-repo")).toBeTruthy());
    fireEvent.click(screen.getByTestId("folder-entry-repo-navigate"));
    await waitFor(() => expect(screen.getByTestId("folder-picker-path")).toHaveTextContent("/home/repo"));
    expect(screen.getByTestId("folder-entry-src")).toBeTruthy();
    fireEvent.click(screen.getByTestId("folder-picker-up"));
    await waitFor(() => expect(screen.getByTestId("folder-picker-path")).toHaveTextContent("/home"));
  });

  it("keeps selected paths across navigation", async () => {
    browseFs.mockResolvedValueOnce(HOME).mockResolvedValueOnce(REPO);
    render(<StatefulPicker />);
    await waitFor(() => expect(screen.getByTestId("folder-entry-new-checkbox")).toBeTruthy());
    fireEvent.click(screen.getByTestId("folder-entry-new-checkbox"));
    await waitFor(() => expect(screen.getByText("1 selected")).toBeTruthy());
    fireEvent.click(screen.getByTestId("folder-entry-repo-navigate"));
    await waitFor(() => expect(screen.getByTestId("folder-picker-path")).toHaveTextContent("/home/repo"));
    fireEvent.click(screen.getByTestId("folder-entry-src-checkbox"));
    await waitFor(() => expect(screen.getByText("2 selected")).toBeTruthy());
  });

  it("renders an error when browseFs rejects", async () => {
    browseFs.mockRejectedValue(new Error("no seat"));
    render(<FolderPicker selected={[]} onChange={() => {}} />);
    await waitFor(() => expect(screen.getByText(/no seat/)).toBeTruthy());
  });
});
