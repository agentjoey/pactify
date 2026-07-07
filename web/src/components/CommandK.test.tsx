import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { CommandK } from "./CommandK";
import type { ProjectMeta, State, Task } from "../lib/types";

vi.mock("../lib/api", () => ({
  postVerb: vi.fn(() => Promise.resolve()),
}));
import { postVerb } from "../lib/api";

const task = (over: Partial<Task> = {}): Task => ({
  id: "t2-cli",
  owner: "opencode",
  status: "awaiting_review",
  reviewer: "claude-opus",
  spec: ".pact/tasks/t2-cli.md",
  evidence: "",
  ...over,
});

const baseState = (tasks: Task[] = [task()]): State => ({
  project: "greet",
  agents: [
    { id: "opencode", roles: ["worker"] },
    { id: "claude-opus", roles: ["orchestrator", "reviewer"] },
  ],
  features: [{ id: "greet-cli", branch: "feat/greet-cli", status: "open", tasks }],
  awaiting_count: tasks.filter((t) => t.status === "awaiting_review").length,
});

const projects: ProjectMeta[] = [
  { id: "greet", name: "greet", path: "/g", project: "greet", feature_count: 1, awaiting_count: 1 },
  { id: "pactify", name: "pactify", path: "/p", project: "pactify", feature_count: 0, awaiting_count: 0 },
];

function setup(over: Partial<Parameters<typeof CommandK>[0]> = {}) {
  const setSelected = vi.fn();
  const onSelectProject = vi.fn();
  const props = {
    projects,
    current: "greet",
    state: baseState(),
    setSelected,
    onSelectProject,
    author: true,
    replaying: false,
    ...over,
  };
  render(<CommandK {...props} />);
  return { setSelected, onSelectProject };
}

function pressCmdK() {
  fireEvent.keyDown(window, { key: "k", metaKey: true });
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("CommandK — command palette", () => {
  it("is closed initially and opens on ⌘K", async () => {
    setup();
    expect(screen.queryByTestId("cmdk")).toBeNull();
    pressCmdK();
    expect(await screen.findByTestId("cmdk")).toBeTruthy();
  });

  it("opens on the pactify:cmdk custom event (TopBar hint)", async () => {
    setup();
    fireEvent(window, new CustomEvent("pactify:cmdk"));
    expect(await screen.findByTestId("cmdk")).toBeTruthy();
  });

  it("does not open on ⌘K when another dialog is open", () => {
    // A foreign dialog (detail panel / modal) present in the DOM.
    const foreign = document.createElement("div");
    foreign.setAttribute("role", "dialog");
    document.body.appendChild(foreign);
    setup();
    pressCmdK();
    expect(screen.queryByTestId("cmdk")).toBeNull();
    foreign.remove();
  });

  it("search filters tasks by id", async () => {
    setup({
      state: baseState([
        task({ id: "t2-cli" }),
        task({ id: "t9-other", status: "in_progress" }),
      ]),
    });
    pressCmdK();
    await screen.findByTestId("cmdk");
    const input = screen.getByPlaceholderText(/Jump to a task/i);
    fireEvent.change(input, { target: { value: "t9-other" } });
    await waitFor(() => {
      expect(screen.queryByText("t9-other")).toBeTruthy();
      expect(screen.queryByText("t2-cli")).toBeNull();
    });
  });

  it("↵ on a task navigates: setSelected(id)", async () => {
    const { setSelected } = setup();
    pressCmdK();
    const palette = await screen.findByTestId("cmdk");
    const item = palette.querySelector('[cmdk-item][data-value="task t2-cli"]') as HTMLElement;
    fireEvent.click(item);
    expect(setSelected).toHaveBeenCalledWith("t2-cli");
    // palette closes after navigation
    await waitFor(() => expect(screen.queryByTestId("cmdk")).toBeNull());
  });

  it("Accept action posts the accept verb (mock api)", async () => {
    setup();
    pressCmdK();
    const palette = await screen.findByTestId("cmdk");
    const accept = within(palette).getByText(/^Accept/).closest("[cmdk-item]") as HTMLElement;
    fireEvent.click(accept);
    await waitFor(() => expect(postVerb).toHaveBeenCalledWith("greet", "accept", { task: "t2-cli" }));
  });

  it("hides the Actions group when observing (author=false)", async () => {
    setup({ author: false });
    pressCmdK();
    const palette = await screen.findByTestId("cmdk");
    expect(within(palette).queryByText(/^Accept/)).toBeNull();
    expect(within(palette).queryByText("t2-cli")).toBeTruthy(); // tasks still listed
  });

  it("hides the Actions group while replaying", async () => {
    setup({ replaying: true });
    pressCmdK();
    const palette = await screen.findByTestId("cmdk");
    expect(within(palette).queryByText(/^Accept/)).toBeNull();
  });

  it("project switch entry calls onSelectProject", async () => {
    const { onSelectProject } = setup();
    pressCmdK();
    const palette = await screen.findByTestId("cmdk");
    const entry = within(palette).getByText("pactify").closest("[cmdk-item]") as HTMLElement;
    fireEvent.click(entry);
    expect(onSelectProject).toHaveBeenCalledWith("pactify");
  });


  it("Esc closes the palette", async () => {
    setup();
    pressCmdK();
    const palette = await screen.findByTestId("cmdk");
    fireEvent.keyDown(palette, { key: "Escape" });
    await waitFor(() => expect(screen.queryByTestId("cmdk")).toBeNull());
  });

  it("lists recipes as commands and fires onRunRecipe on select", async () => {
    const onRunRecipe = vi.fn();
    setup({
      recipes: [{ name: "review-harden", description: "implement + harden" }],
      onRunRecipe,
    });
    pressCmdK();
    await screen.findByTestId("cmdk");
    const input = screen.getByPlaceholderText(/Jump to a task/i);
    fireEvent.change(input, { target: { value: "harden" } });
    await waitFor(() => expect(screen.getByText("review-harden")).toBeInTheDocument());
    fireEvent.click(screen.getByText("review-harden"));
    expect(onRunRecipe).toHaveBeenCalledWith("review-harden");
  });
});

describe("CommandK — cheat sheet", () => {
  it("? opens the shortcut cheat sheet", async () => {
    setup();
    fireEvent.keyDown(window, { key: "?" });
    expect(await screen.findByTestId("cheat-sheet")).toBeTruthy();
    expect(screen.getByText("Command palette")).toBeTruthy();
  });

  it("? is typing-guarded (ignored while focused in an input)", () => {
    setup();
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();
    fireEvent.keyDown(input, { key: "?" });
    expect(screen.queryByTestId("cheat-sheet")).toBeNull();
    input.remove();
  });

  it("? does not open while a dialog is already open", () => {
    const foreign = document.createElement("div");
    foreign.setAttribute("role", "dialog");
    document.body.appendChild(foreign);
    setup();
    fireEvent.keyDown(window, { key: "?" });
    expect(screen.queryByTestId("cheat-sheet")).toBeNull();
    foreign.remove();
  });
});
