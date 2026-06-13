import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";
import { TopBar, type View } from "./TopBar";
import type { ProjectMeta, Seat } from "../lib/types";

function pm(id: string, name = id): ProjectMeta {
  return { id, name, path: `/${id}`, project: id, feature_count: 0, awaiting_count: 0 };
}

const PROJECTS = [pm("greet"), pm("billing"), pm("inbox")];
const AGENTS: Seat[] = [
  { id: "claude-opus", roles: ["orchestrator"] },
  { id: "worker-1", roles: ["worker"] },
];

function setup(overrides: Partial<React.ComponentProps<typeof TopBar>> = {}) {
  const onSelect = vi.fn();
  const onView = vi.fn();
  const props = {
    projects: PROJECTS,
    current: "greet",
    onSelect,
    live: true,
    replaying: false,
    view: "kanban" as View,
    onView,
    author: true,
    seat: "claude-opus",
    agents: AGENTS,
    ...overrides,
  };
  render(<TopBar {...props} />);
  return { onSelect, onView };
}

describe("TopBar", () => {
  it("renders logo, wordmark, and the current project chip", () => {
    setup();
    expect(screen.getByText("pactify")).toBeInTheDocument();
    expect(screen.getByTestId("project-chip")).toHaveTextContent("greet");
  });

  describe("project popover", () => {
    it("opens, filters by query, and selects", () => {
      const { onSelect } = setup();
      fireEvent.click(screen.getByTestId("project-chip"));
      const list = screen.getByRole("listbox");
      // all projects listed initially
      expect(within(list).getAllByRole("option")).toHaveLength(3);
      // filter
      fireEvent.change(screen.getByLabelText("search projects"), { target: { value: "bill" } });
      const opts = within(list).getAllByRole("option");
      expect(opts).toHaveLength(1);
      expect(opts[0]).toHaveTextContent("billing");
      // select
      fireEvent.click(opts[0]);
      expect(onSelect).toHaveBeenCalledWith("billing");
    });

    it("shows no-match when the query matches nothing", () => {
      setup();
      fireEvent.click(screen.getByTestId("project-chip"));
      fireEvent.change(screen.getByLabelText("search projects"), { target: { value: "zzz" } });
      expect(screen.getByText("no match")).toBeInTheDocument();
    });
  });

  describe("segmented control", () => {
    it("marks the active view and shows 1/2/3 kbd hints", () => {
      setup({ view: "canvas" });
      const group = screen.getByRole("group", { name: "view toggle" });
      const buttons = within(group).getAllByRole("button");
      expect(buttons.map((b) => b.getAttribute("aria-pressed"))).toEqual(["false", "true", "false", "false"]);
      ["1", "2", "3", "4"].forEach((k) => expect(within(group).getByText(k)).toBeInTheDocument());
    });

    it("fires onView on click", () => {
      const { onView } = setup();
      const group = screen.getByRole("group", { name: "view toggle" });
      fireEvent.click(within(group).getByText("Ops"));
      expect(onView).toHaveBeenCalledWith("ops");
    });
  });

  describe("⌘K hint", () => {
    it("dispatches the pactify:cmdk event on click", () => {
      setup();
      const handler = vi.fn();
      window.addEventListener("pactify:cmdk", handler);
      fireEvent.click(screen.getByTestId("cmdk-hint"));
      expect(handler).toHaveBeenCalledTimes(1);
      window.removeEventListener("pactify:cmdk", handler);
    });
  });

  describe("live badge", () => {
    it("live state", () => {
      setup({ live: true, replaying: false });
      expect(screen.getByTestId("live-badge")).toHaveAttribute("data-state", "live");
    });
    it("replay state takes precedence over live", () => {
      setup({ live: true, replaying: true });
      expect(screen.getByTestId("live-badge")).toHaveAttribute("data-state", "replay");
    });
    it("offline state", () => {
      setup({ live: false, replaying: false });
      expect(screen.getByTestId("live-badge")).toHaveAttribute("data-state", "offline");
    });
  });

  describe("seat / observe", () => {
    it("renders the acting-seat ant when author", () => {
      setup({ author: true, seat: "claude-opus" });
      expect(screen.getByTestId("seat-avatar")).toBeInTheDocument();
      expect(screen.queryByTestId("observe-badge")).not.toBeInTheDocument();
      // caste resolved from roster: orchestrator → queen
      expect(screen.getByTitle(/acting as claude-opus — queen/)).toBeInTheDocument();
    });

    it("renders the observe badge when not author", () => {
      setup({ author: false, seat: "" });
      expect(screen.getByTestId("observe-badge")).toBeInTheDocument();
      expect(screen.getByText("👁 observing")).toBeInTheDocument();
      expect(screen.queryByTestId("seat-avatar")).not.toBeInTheDocument();
    });
  });
});
