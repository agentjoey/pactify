import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
vi.mock("../../buildTarget", () => ({ panelBuild: true }));
import { Settings } from "./Settings";
import type { JobsSource } from "./source";

afterEach(cleanup);

it("hides Refresh models in the panel build even when source exposes write", async () => {
  const write = vi.fn(async () => Response.json({ harnesses: [] }));
  const source: JobsSource = {
    jobsPath: "/jobs",
    costPath: "/cost",
    routerPath: "/router",
    harnessesPath: "/harnesses",
    fetch: vi.fn(async (path: string) => {
      if (path.includes("harnesses")) {
        return Response.json({
          harnesses: [
            {
              name: "codex",
              installed: true,
              version: "0.162.1",
              verifiedVersion: "0.162.1",
              experimental: false,
              readOnly: true,
              efforts: ["low", "high"],
              default: { model: "gpt-5" },
              models: { items: ["gpt-5"], error: "" },
              stale: true,
            },
          ],
        });
      }
      if (path.endsWith("doctor")) return Response.json({ lastRun: null, harnesses: [] });
      return Response.json({
        version: "v1",
        config: { defaults: {}, aliases: {}, fallback: {}, quotaGroups: {} },
      });
    }),
    write,
  };
  render(<Settings source={source} />);
  expect(await screen.findByRole("article", { name: "codex" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Refresh models" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Save defaults" })).toBeNull();
  expect(write).not.toHaveBeenCalled();
});
