import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const postRegister = vi.fn();
vi.mock("../lib/api", () => ({
  postRegister: (...args: unknown[]) => postRegister(...args),
}));

const isHostedMode = vi.fn(() => false);
vi.mock("../lib/source", () => ({
  isHostedMode: () => isHostedMode(),
}));

import { NoProjects } from "./NoProjects";

describe("NoProjects hero (local mode)", () => {
  beforeEach(() => { postRegister.mockReset(); isHostedMode.mockReturnValue(false); });

  it("renders the hero copy, cable mark and register form", () => {
    render(<NoProjects onRegistered={() => {}} />);
    expect(screen.getByTestId("no-projects")).toBeTruthy();
    expect(screen.getByText("No repos connected yet")).toBeTruthy();
    expect(screen.getByTestId("no-projects-form")).toBeTruthy();
    // cable mark svg is present inside the hero
    expect(screen.getByTestId("no-projects").querySelector("svg")).toBeTruthy();
  });

  it("Register button is disabled until a path is entered", () => {
    render(<NoProjects onRegistered={() => {}} />);
    const btn = screen.getByRole("button", { name: "Register" }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("path"), { target: { value: "/repo" } });
    expect(btn.disabled).toBe(false);
  });

  it("registers via postRegister and calls onRegistered on success", async () => {
    postRegister.mockResolvedValue({ name: "repo" });
    const onRegistered = vi.fn();
    render(<NoProjects onRegistered={onRegistered} />);
    fireEvent.change(screen.getByLabelText("path"), { target: { value: "  /abs/repo  " } });
    fireEvent.change(screen.getByLabelText("name"), { target: { value: "myrepo" } });
    fireEvent.click(screen.getByRole("button", { name: "Register" }));
    await waitFor(() => expect(onRegistered).toHaveBeenCalledTimes(1));
    // path is trimmed, name passed through
    expect(postRegister).toHaveBeenCalledWith("/abs/repo", "myrepo");
  });

  it("humanizes a registration error and does not call onRegistered", async () => {
    // Gate the rejection on a promise the test releases INSIDE an act() flush:
    // the component's `await postRegister()` suspends on the gate, then the throw
    // (and the resulting setErr) happen within React's act scope, so the
    // rejection is fully handled and never escapes to the worker as "unhandled".
    let release!: () => void;
    const gate = new Promise<void>((r) => { release = r; });
    postRegister.mockImplementationOnce(async () => {
      await gate;
      throw new Error('acting seat "x" is not in the project roster');
    });
    const onRegistered = vi.fn();
    render(<NoProjects onRegistered={onRegistered} />);
    fireEvent.change(screen.getByLabelText("path"), { target: { value: "/repo" } });
    fireEvent.click(screen.getByText("Register"));
    await act(async () => { release(); await gate; });
    await waitFor(() => {
      const err = screen.getByTestId("no-projects-error");
      expect(err).toHaveTextContent("当前坐席不在该项目的 roster");
      // raw kept in parens
      expect(err).toHaveTextContent("is not in the project roster");
    });
    expect(onRegistered).not.toHaveBeenCalled();
  });
});

describe("NoProjects hero (hosted mode)", () => {
  beforeEach(() => { postRegister.mockReset(); isHostedMode.mockReturnValue(true); });

  it("shows hosted guidance instead of the local register form", () => {
    render(<NoProjects onRegistered={() => {}} />);
    expect(screen.getByTestId("no-projects")).toBeTruthy();
    expect(screen.getByText("No projects yet on your account")).toBeTruthy();
    // hosted guidance panel present; the local-path register form is NOT.
    expect(screen.getByTestId("no-projects-hosted-guidance")).toBeTruthy();
    expect(screen.queryByTestId("no-projects-form")).toBeNull();
    expect(screen.queryByRole("button", { name: "Register" })).toBeNull();
    expect(screen.queryByLabelText("path")).toBeNull();
    // guidance names the machine-connect command.
    expect(screen.getByText(/pactify serve/)).toBeTruthy();
    // still keeps the CableMark hero.
    expect(screen.getByTestId("no-projects").querySelector("svg")).toBeTruthy();
  });

  it("never calls postRegister in hosted mode (no register path)", () => {
    render(<NoProjects onRegistered={() => {}} />);
    expect(postRegister).not.toHaveBeenCalled();
  });
});
