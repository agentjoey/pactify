import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Settings } from "./Settings";
import type { JobsSource } from "./source";
afterEach(cleanup);
const snapshot = {
  version: "v1", path: "/router", exists: true, exhaustedRoutes: [],
  config: { defaults: {}, aliases: { fast: { harness: "agy", model: "argon-low" } }, fallback: {}, quotaGroups: { argon: { harness: "agy", models: ["argon-low"] } } },
};
const harness = { name: "agy", installed: true, version: "1.4.0", verifiedVersion: "1.4.0", efforts: [], default: {}, models: { items: ["argon-low", "argon-high"], error: "" }, stale: true };
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}
function fixture() {
  const harnesses = deferred<Response>();
  const defaults = deferred<Response>();
  let routerCalls = 0;
  const source: JobsSource = {
    jobsPath: "/jobs", costPath: "/cost", harnessesPath: "/harnesses", routerPath: "/router",
    fetch: vi.fn((path: string) => {
      if (path === "/storage") return Promise.resolve(Response.json({ policy: { olderThan: "336h", cacheMaxBytes: 5000000000, interval: "24h" }, locations: [], staleWorktrees: [], failures: [], lastRun: null }));
      if (path === "/doctor") return Promise.resolve(Response.json({lastRun:null,harnesses:[]}));
      if (path.includes("harnesses")) return harnesses.promise;
      // Routing can load before Settings' independent defaults request completes.
      if (routerCalls++ === 1) return defaults.promise;
      return Promise.resolve(Response.json(snapshot));
    }),
    write: vi.fn(async () => Response.json({})),
  };
  render(<Settings source={source} />);
  return { harnesses, defaults };
}
it("renders routing before harnesses and keeps Edit through late defaults and catalog arrival", async () => {
  const { harnesses, defaults } = fixture();
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  expect(screen.getByText("Loading harnesses…")).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Alias 1 name"), { target: { value: "my-draft" } });
  await act(async () => { defaults.resolve(Response.json(snapshot)); });
  // This catches reintroducing key={snapshot?.version || "initial"}.
  expect(screen.getByLabelText("Alias 1 name")).toHaveValue("my-draft");
  expect(screen.getByRole("combobox", { name: "Alias 1 model" })).toBeDisabled();
  await act(async () => { harnesses.resolve(Response.json({ harnesses: [harness] })); });
  expect(screen.getByLabelText("Alias 1 name")).toHaveValue("my-draft");
  const model = screen.getByRole("combobox", { name: "Alias 1 model" });
  expect(model).toBeEnabled();
  expect(model).toHaveValue("argon-low");
  expect(within(model).getByRole("option", { name: "argon-high" })).toBeInTheDocument();
  expect(screen.getByRole("combobox", { name: "Quota group 1 model 1" })).toBeEnabled();
  expect(screen.queryByText("Loading harnesses…")).not.toBeInTheDocument();
});
it("uses disabled loading selects for alias and quota models", async () => {
  fixture();
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  for (const name of ["Alias 1 model", "Quota group 1 model 1"]) {
    const model = screen.getByRole("combobox", { name });
    expect(model).toBeDisabled();
    expect(model).toHaveTextContent("Loading models…");
    expect(screen.queryByRole("textbox", { name })).not.toBeInTheDocument();
  }
});
it("shows a muted cached hint beside Refresh models", async () => {
  const { harnesses, defaults } = fixture();
  await act(async () => { defaults.resolve(Response.json(snapshot)); harnesses.resolve(Response.json({ harnesses: [harness] })); });
  const refresh = screen.getByRole("button", { name: "Refresh models" });
  expect(refresh.nextElementSibling).toHaveTextContent("cached");
  expect(refresh.nextElementSibling).toHaveClass("panel-caption");
});
it("keeps loaded empty model lists as free text", async () => {
  const { harnesses } = fixture();
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  await act(async () => { harnesses.resolve(Response.json({ harnesses: [{ ...harness, models: { items: [], error: "" } }] })); });
  expect(screen.getByRole("textbox", { name: "Alias 1 model" })).toHaveValue("argon-low");
  expect(screen.getByRole("textbox", { name: "agy Default model" })).toBeInTheDocument();
});
it("keeps routing usable when harness discovery fails", async () => {
  const { harnesses } = fixture();
  await act(async () => { harnesses.resolve(Response.json({}, { status: 500 })); });
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  expect(screen.getByRole("textbox", { name: "Alias 1 model" })).toBeEnabled();
  expect(screen.queryByText("Loading harnesses…")).not.toBeInTheDocument();
  expect(screen.getByRole("alert")).toBeInTheDocument();
});
it("keeps harnesses visible when defaults fail", async () => {
  const { harnesses, defaults } = fixture();
  await act(async () => { defaults.resolve(Response.json({}, { status: 500 })); harnesses.resolve(Response.json({ harnesses: [harness] })); });
  expect(screen.getByLabelText("agy Default model")).toBeInTheDocument();
  expect(screen.getByRole("alert")).toBeInTheDocument();
});

it("shows experimental labels only for agy and keeps kimi read-only unavailable", async () => {
  const { harnesses, defaults } = fixture();
  await act(async () => {
    defaults.resolve(Response.json(snapshot));
    harnesses.resolve(Response.json({ harnesses: ["codex", "kimi", "agy", "opencode"].map((name) => ({
      ...harness, name, experimental: name === "agy", readOnly: name !== "kimi",
    })) }));
  });
  for (const name of ["codex", "kimi", "agy", "opencode"]) {
    const card = within(screen.getByRole("article", { name }));
    expect(card.queryByText("experimental") !== null).toBe(name === "agy");
    expect(card.getByText(`Read-only: ${name === "kimi" ? "Not supported" : "Supported"}`)).toBeInTheDocument();
  }
});

it("keeps the exact Defaults saved confirmation visible after saving", async () => {
  const source: JobsSource = {
    jobsPath: "/jobs", costPath: "/cost", routerPath: "/router", harnessesPath: "/harnesses",
    fetch: vi.fn(async path => Response.json(path.includes("harnesses") ? {harnesses:[harness]} : path.endsWith("doctor") ? {lastRun:null,harnesses:[]} : snapshot)),
    write: vi.fn(async () => Response.json({})),
  };
  render(<Settings source={source} />);
  fireEvent.click(await screen.findByRole("button", {name:"Save defaults"}));
  expect(await screen.findByText("Defaults saved", {exact:true})).toBeVisible();
});
