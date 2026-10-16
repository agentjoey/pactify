import { expect, it } from "vitest";
import { createJobsAPI, seedJobs } from "../../../e2e/jobs-fixtures.mjs";
import { mergeJobChains } from "./board";
import type { Job } from "./types";

it("keeps every seeded job and reset independent, including nested arrays", () => {
  const first = seedJobs();
  const second = seedJobs();
  first.find((j) => j.id === "job-failed")!.resumedBy.push("child");
  expect(first.find((j) => j.id === "job-queued")!.resumedBy).toEqual([]);
  expect(second.find((j) => j.id === "job-failed")!.resumedBy).toEqual([]);
  expect(seedJobs().find((j) => j.id === "job-failed")!.resumedBy).toEqual([]);
});
it("resumes only its parent chain, refreshes two queued cards, and resets cleanly", async () => {
  const api = createJobsAPI(true);
  async function request(path: string, method = "GET", body = {}) {
    let value: unknown;
    let status = 0;
    const req = {
      url: path,
      method,
      async *[Symbol.asyncIterator]() {
        yield JSON.stringify(body);
      },
    };
    const res = {
      writeHead(code: number) {
        status = code;
      },
      end(text: string) {
        value = JSON.parse(text);
      },
    };
    expect(await api(req, res, path)).toBe(true);
    expect(status).toBe(200);
    return value;
  }
  expect(
    await request("/api/jobs/job-failed/resume", "POST", {
      brief: "Fix the remaining checks\nRun all tests",
    }),
  ).toEqual({ jobId: "job-resumed-1" });
  const rows = (await request("/api/jobs")) as Job[];
  const queued = mergeJobChains(rows).filter(
    (c) => c.latest.status === "queued",
  );
  expect(queued.map((c) => c.root.title).sort()).toEqual([
    "Fix CLI exit code edge cases",
    "Queued implementation",
  ]);
  expect(queued.find((c) => c.root.id === "job-failed")?.rework).toBe(1);
  expect(
    rows.filter((j) => j.resumedBy.includes("job-resumed-1")).map((j) => j.id),
  ).toEqual(["job-failed"]);
  api.reset();
  expect(
    ((await request("/api/jobs")) as Job[]).every(
      (j) => !j.resumedBy.includes("job-resumed-1"),
    ),
  ).toBe(true);
});
