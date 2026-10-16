import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Routing } from "./Routing";
afterEach(cleanup);
it("preserves harness defaults when saving routing edits", async () => {
  const config = {
    defaults: { codex: { model: "gpt-6.1-sol", effort: "high" } },
    aliases: {},
    fallback: {},
    quotaGroups: {},
  };
  const write = vi.fn(
    async (_path: string, _init: RequestInit) => new Response("{}"),
  );
  render(
    <Routing
      expanded
      source={{
        jobsPath: "/jobs",
        costPath: "/cost",
        routerPath: "/router",
        write,
        fetch: async () =>
          new Response(
            JSON.stringify({ config, version: "v1", exhaustedRoutes: [] }),
          ),
      }}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(write).toHaveBeenCalled());
  expect(
    JSON.parse(write.mock.calls[0][1].body as string).config.defaults,
  ).toEqual(config.defaults);
});
it("picks alias and quota group models from the harness catalog", async () => {
  const config = {
    aliases: { mm: { harness: "opencode", model: "minimax/M3" } },
    fallback: {},
    quotaGroups: { argon: { harness: "agy", models: ["argon-low"] } },
  };
  const write = vi.fn(
    async (_path: string, _init: RequestInit) => new Response("{}"),
  );
  render(
    <Routing
      expanded
      catalog={{
        opencode: { models: ["minimax/M3", "deepseek/flash"], efforts: [] },
        codex: { models: ["gpt-6.1-sol"], efforts: ["low", "high"] },
        agy: { models: ["argon-low", "argon-high"], efforts: [] },
      }}
      source={{
        jobsPath: "/jobs",
        costPath: "/cost",
        routerPath: "/router",
        write,
        fetch: async () =>
          new Response(
            JSON.stringify({ config, version: "v1", exhaustedRoutes: [] }),
          ),
      }}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  const model = screen.getByRole("combobox", { name: "Alias 1 model" });
  expect(
    [...(model as HTMLSelectElement).options].map((o) => o.textContent),
  ).toEqual(["Select a model", "minimax/M3", "deepseek/flash"]);
  // No efforts reported for opencode: effort stays free text.
  expect(screen.getByRole("textbox", { name: "Alias 1 effort" })).toBeTruthy();

  // Switching harness clears the model and offers that harness's choices.
  fireEvent.change(screen.getByRole("combobox", { name: "Alias 1 harness" }), {
    target: { value: "codex" },
  });
  expect(
    (
      screen.getByRole("combobox", {
        name: "Alias 1 model",
      }) as HTMLSelectElement
    ).value,
  ).toBe("");
  fireEvent.change(screen.getByRole("combobox", { name: "Alias 1 model" }), {
    target: { value: "gpt-6.1-sol" },
  });
  fireEvent.change(screen.getByRole("combobox", { name: "Alias 1 effort" }), {
    target: { value: "high" },
  });
  fireEvent.change(
    screen.getByRole("combobox", { name: "Quota group 1 model 1" }),
    { target: { value: "argon-high" } },
  );
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(write).toHaveBeenCalled());
  const saved = JSON.parse(write.mock.calls[0][1].body as string).config;
  expect(saved.aliases.mm).toEqual({
    harness: "codex",
    model: "gpt-6.1-sol",
    effort: "high",
  });
  expect(saved.quotaGroups.argon.models).toEqual(["argon-high"]);
});
