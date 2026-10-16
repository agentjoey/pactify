import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Routing } from "./Routing";
import type { JobsSource } from "./source";

const snapshot = {
  path: "/tmp/pactify/router.json",
  exists: true,
  version: "original",
  config: {
    aliases: { fast: { harness: "codex", model: "gpt-test", effort: "low" } },
    fallback: { codex: ["@fast", "kimi"] },
    quotaGroups: { shared: { harness: "agy", models: ["one", "two"] } },
  },
  exhaustedRoutes: [
    {
      key: "codex/gpt-test",
      harness: "codex",
      model: "gpt-test",
      resetAt: new Date(Date.now() + 3600000).toISOString(),
    },
  ],
};
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});
function setup(status = 200, writable = true) {
  const read = vi.fn(async () => new Response(JSON.stringify(snapshot)));
  const write = vi.fn(
    async () =>
      new Response(
        JSON.stringify(
          status === 422
            ? {
                errors: [
                  { field: "aliases.fast.model", message: "Invalid model" },
                ],
              }
            : {},
        ),
        { status },
      ),
  );
  const source: JobsSource = {
    jobsPath: "/jobs",
    costPath: "/cost",
    routerPath: "/router",
    fetch: read,
    ...(writable ? { write } : {}),
  };
  render(<Routing source={source} />);
  fireEvent.click(screen.getByText("Routing", { exact: true }));
  return { read, write };
}
it("renders configuration, quota countdown and the panel read-only hint", async () => {
  const { write } = setup(200, false);
  expect(await screen.findByText("@fast")).toBeInTheDocument();
  expect(screen.getByText("gpt-test")).toBeInTheDocument();
  expect(screen.getByText("@fast → kimi")).toBeInTheDocument();
  expect(screen.getByText("one, two")).toBeInTheDocument();
  expect(screen.getByText(/resets in (\d+h )?\d+m/)).toBeInTheDocument();
  expect(
    screen.getByText(
      "Edit routing from the dashboard or with pactify sub router.",
    ),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Edit" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Clear" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  expect(write).not.toHaveBeenCalled();
});
it("edits aliases, ordered fallback and quota groups and saves the original version", async () => {
  const { write } = setup();
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  fireEvent.change(screen.getByLabelText("Alias 1 model"), {
    target: { value: "new-model" },
  });
  fireEvent.change(screen.getByLabelText("Alias 1 effort"), {
    target: { value: "high" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Add alias" }));
  fireEvent.change(screen.getByLabelText("Alias 2 name"), {
    target: { value: "second" },
  });
  fireEvent.change(screen.getByLabelText("Alias 2 harness"), {
    target: { value: "opencode" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Move codex route 2 up" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Add route to codex" }));
  fireEvent.change(screen.getByLabelText("codex route 3"), {
    target: { value: "@second" },
  });
  fireEvent.change(screen.getByLabelText("Quota group 1 model 2"), {
    target: { value: "three" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(write).toHaveBeenCalledTimes(1));
  const [path, init] = write.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe("/router");
  expect(init.method).toBe("PUT");
  expect(JSON.parse(init.body as string)).toEqual({
    version: "original",
    config: {
      aliases: {
        fast: { harness: "codex", model: "new-model", effort: "high" },
        second: { harness: "opencode", model: "", effort: "" },
      },
      fallback: { codex: ["kimi", "@fast", "@second"] },
      quotaGroups: { shared: { harness: "agy", models: ["one", "three"] } },
    },
  });
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "Save" }),
    ).not.toBeInTheDocument(),
  );
});
it("shows 422 errors beside the corresponding field and keeps editing", async () => {
  setup(422);
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await screen.findAllByText("Invalid model");
  expect(
    screen.getByLabelText("Alias 1 model").closest("label"),
  ).toHaveTextContent("Invalid model");
  expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
});
it("reports a version conflict and reloads the latest configuration", async () => {
  const { read } = setup(409);
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(
    await screen.findByText(
      "Config changed elsewhere — reload to see the latest",
    ),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Reload" }));
  await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
  expect(
    screen.queryByRole("button", { name: "Save" }),
  ).not.toBeInTheDocument();
});
it("requires an inline confirmation for Clear, can cancel, then clears the encoded key", async () => {
  const { write, read } = setup();
  fireEvent.click(await screen.findByRole("button", { name: "Clear" }));
  expect(write).not.toHaveBeenCalled();
  let dialog = screen.getByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(write).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Clear" }));
  dialog = screen.getByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "Clear" }));
  await waitFor(() =>
    expect(write).toHaveBeenCalledWith(
      "/quota/codex%2Fgpt-test",
      expect.objectContaining({ method: "DELETE" }),
    ),
  );
  await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
});
it("can remove entries, cancel changes and prevents duplicate names from overwriting data", async () => {
  const { write } = setup();
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  fireEvent.click(screen.getByRole("button", { name: "Add alias" }));
  fireEvent.change(screen.getByLabelText("Alias 2 name"), {
    target: { value: "fast" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(await screen.findByText("Duplicate alias name")).toBeInTheDocument();
  expect(write).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Remove alias 2" }));
  fireEvent.click(screen.getByRole("button", { name: "Remove codex route 1" }));
  fireEvent.click(screen.getByRole("button", { name: "Remove quota group 1" }));
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.getByText("@fast → kimi")).toBeInTheDocument();
  expect(screen.getByText("one, two")).toBeInTheDocument();
});
it("preserves model names containing commas and lets quota models be added and removed", async () => {
  const read = vi.fn(
    async () =>
      new Response(
        JSON.stringify({
          ...snapshot,
          config: {
            ...snapshot.config,
            quotaGroups: {
              shared: { harness: "agy", models: ["one,two", "three"] },
            },
          },
        }),
      ),
  );
  const write = vi.fn(async () => new Response("{}"));
  render(
    <Routing
      source={{
        jobsPath: "/jobs",
        costPath: "/cost",
        routerPath: "/router",
        fetch: read,
        write,
      }}
    />,
  );
  fireEvent.click(screen.getByText("Routing", { exact: true }));
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  expect(screen.getByLabelText("Quota group 1 model 1")).toHaveValue("one,two");
  fireEvent.click(
    screen.getByRole("button", { name: "Remove model 2 from quota group 1" }),
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Add model to quota group 1" }),
  );
  fireEvent.change(screen.getByLabelText("Quota group 1 model 2"), {
    target: { value: "four" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(write).toHaveBeenCalled());
  const [, init] = write.mock.calls[0] as unknown as [string, RequestInit];
  expect(
    JSON.parse(init.body as string).config.quotaGroups.shared.models,
  ).toEqual(["one,two", "four"]);
});
it("closes a pending Clear confirmation when editing begins", async () => {
  setup();
  fireEvent.click(await screen.findByRole("button", { name: "Clear" }));
  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("keeps the edit's original version when an earlier refresh finishes late", async () => {
  vi.useFakeTimers();
  let finishRefresh!: (response: Response) => void;
  const read = vi
    .fn()
    .mockResolvedValueOnce(new Response(JSON.stringify(snapshot)))
    .mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          finishRefresh = resolve;
        }),
    )
    .mockResolvedValue(new Response(JSON.stringify(snapshot)));
  const write = vi.fn(async () => new Response("{}"));
  render(
    <Routing
      source={{
        jobsPath: "/jobs",
        costPath: "/cost",
        routerPath: "/router",
        fetch: read,
        write,
      }}
    />,
  );
  fireEvent.click(screen.getByText("Routing", { exact: true }));
  await act(async () => {});
  await act(async () => {
    await vi.advanceTimersByTimeAsync(30000);
  });
  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  await act(async () => {
    finishRefresh(
      new Response(
        JSON.stringify({ ...snapshot, version: "changed-elsewhere" }),
      ),
    );
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await act(async () => {});
  const [, init] = write.mock.calls[0] as unknown as [string, RequestInit];
  expect(JSON.parse(init.body as string).version).toBe("original");
  vi.useRealTimers();
});
