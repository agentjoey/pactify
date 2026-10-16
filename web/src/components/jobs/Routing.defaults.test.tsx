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
