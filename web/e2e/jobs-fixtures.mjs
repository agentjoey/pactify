// Mutable, hermetic job API shared by dashboard and read-only panel fixtures.
const base = {
  effort: "high",
  harness: "codex",
  modelRequested: "gpt-6.1-sol",
  modelActual: "gpt-6.1-sol",
  repo: "/workspace/pactify",
  cwd: "/workspace/pactify",
  access: "write",
  configMode: "minimal",
  status: "succeeded",
  duration: "3m42s",
  inputTokens: 12480,
  outputTokens: 3260,
  attention: [],
  resumeOf: "",
  resumedBy: [],
  actions: ["resume"],
};
export function seedJobs() {
  const now = Date.now();
  const ago = (minutes) => new Date(now - minutes * 60000).toISOString();
  // Clone each job separately: cloning the whole array would preserve aliases from base.
  return [
    {
      ...base,
      id: "job-panel",
      title: "Build a local job panel",
      createdAt: ago(30),
      finishedAt: ago(20),
      resumedBy: ["job-review"],
      actions: [],
    },
    {
      ...base,
      id: "job-review",
      title: "Panel acceptance and visual checks",
      status: "running",
      createdAt: ago(5),
      startedAt: ago(5),
      resumeOf: "job-panel",
      actions: ["cancel"],
      activity: {
        summary: "tool exec go test ./internal/subpanel/...",
        inputTokens: 8420,
        outputTokens: 1870,
        usagePartial: true,
      },
    },
    {
      ...base,
      id: "job-summary",
      title: "Share job summaries",
      createdAt: ago(60),
      finishedAt: ago(50),
      attention: [
        {
          kind: "worktree_pending",
          message: "Worktree is awaiting apply or discard",
        },
      ],
      worktree: {
        path: "/tmp/pactify/jobs/job-summary/wt",
        baseCommit: "97e9d6bf2143",
        status: "pending",
      },
      actions: ["resume", "apply", "discard", "dismiss"],
    },
    {
      ...base,
      id: "job-failed",
      title: "Fix CLI exit code edge cases",
      status: "failed",
      createdAt: ago(70),
      finishedAt: ago(65),
      failureClass: "harness",
      attention: [{ kind: "failed", message: "Job failed" }],
    },
    {
      ...base,
      id: "job-kimi",
      title: "Evaluate resume for large tasks",
      harness: "kimi",
      modelRequested: "kimi-k2.5",
      modelActual: "kimi-k2.5",
      repo: "/workspace/harness-eval",
      createdAt: ago(120),
      finishedAt: new Date(now).toISOString(),
    },
    ...Array.from({ length: 5 }, (_, i) => ({
      ...base,
      id: `job-done-${i}`,
      title: `Completed task ${i}`,
      createdAt: ago(130 + i),
      finishedAt: new Date(now).toISOString(),
    })),
    {
      ...base,
      id: "job-quota",
      title: "Recover a quota-limited task",
      status: "failed",
      failureClass: "quota",
      fallbackTo: "job-fallback",
      createdAt: ago(1600),
      finishedAt: ago(1595),
      actions: [],
    },
    {
      ...base,
      id: "job-fallback",
      title: "Fallback attempt",
      harness: "opencode",
      modelRequested: "deepseek-flash",
      modelActual: "deepseek-flash",
      fallbackFrom: "job-quota",
      createdAt: ago(1590),
      finishedAt: ago(1585),
    },
  ].map((job) => structuredClone(job));
}
const quotaReset = (hours) => {
  const date = new Date(Date.now() + hours * 3600000);
  const pad = (value) => String(value).padStart(2, "0");
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
};
const send = (res, status, value) => {
  res.writeHead(status, {
    "Content-Type": "application/json",
    "Cache-Control": "no-store",
  });
  res.end(JSON.stringify(value));
};
// A long model id checks that model pickers never widen the page.
export const LONG_MODEL = "model/" + "long-name".repeat(15);
export function createJobsAPI(writable = false) {
  let jobs = seedJobs(),
    next = 0;
  const handler = async (req, res, path) => {
    if (!/^\/api\/(jobs(?:\/|$)|cost$|harnesses$)/.test(path)) return false;
    const url = new URL(req.url, "http://localhost");
    if (req.method === "GET" && path === "/api/harnesses") {
      send(res, 200, {
        harnesses: ["codex", "kimi", "agy", "opencode"].map((name, i) => ({
          name,
          installed: i !== 2,
          version: "1.0",
          verifiedVersion: i === 0 ? "0.9" : "1.0",
          experimental: name === "agy",
          readOnly: name !== "kimi",
          default: {},
          models: {
            items:
              i === 0
                ? ["gpt-6.1-sol", "gpt-6-sol", LONG_MODEL]
                : i === 3
                  ? ["model-one", "model-two", LONG_MODEL]
                  : ["model-one", "model-two"],
            error: "",
            fetchedAt: new Date().toISOString(),
          },
          efforts: ["low", "medium", "high"],
        })),
      });
      return true;
    }
    if (req.method === "GET" && path === "/api/cost") {
      const groups = [...new Set(jobs.map((j) => j.harness))].map((key) => {
        const rows = jobs.filter((j) => j.harness === key);
        return {
          key,
          jobs: rows.length,
          succeeded: rows.filter((j) => j.status === "succeeded").length,
          failed: rows.filter((j) => j.status === "failed").length,
          inputTokens: rows.reduce((n, j) => n + j.inputTokens, 0),
          outputTokens: rows.reduce((n, j) => n + j.outputTokens, 0),
          costUSD: key === "opencode" ? 0.125 : null,
          subscriptionJobs: key === "opencode" ? 0 : rows.length,
          unpricedJobs: 0,
          usagePartialJobs: 1,
        };
      });
      send(res, 200, {
        by: url.searchParams.get("by") || "harness",
        since: new Date(Date.now() - 7 * 86400000).toISOString(),
        until: new Date().toISOString(),
        groups,
        total: groups.reduce(
          (total, group) => {
            for (const key of [
              "jobs",
              "succeeded",
              "failed",
              "inputTokens",
              "outputTokens",
              "subscriptionJobs",
              "unpricedJobs",
              "usagePartialJobs",
            ])
              total[key] = (total[key] || 0) + group[key];
            if (group.costUSD != null)
              total.costUSD = (total.costUSD || 0) + group.costUSD;
            return total;
          },
          { costUSD: null },
        ),
        quotaSnapshots: [
          {
            harness: "codex",
            jobId: "job-panel",
            observedAt: new Date().toISOString(),
            rateLimit: `5-hour window: 20% used, resets ${quotaReset(2)}; 7-day window: 35% used, resets ${quotaReset(120)}`,
          },
        ],
        exhaustedRoutes: [
          {
            key: "codex/gpt-test",
            harness: "codex",
            model: "gpt-test",
            resetAt: new Date(Date.now() + 7200000).toISOString(),
          },
        ],
      });
      return true;
    }
    if (req.method === "GET" && path === "/api/jobs") {
      send(res, 200, jobs);
      return true;
    }
    const match = path.match(
      /^\/api\/jobs\/([^/]+)(?:\/(logs|events|diff|cancel|resume|apply|discard))?$/,
    );
    const job =
      match && jobs.find((j) => j.id === decodeURIComponent(match[1]));
    if (!job) {
      send(res, 404, { error: "Job not found" });
      return true;
    }
    const action = match[2];
    if (
      req.method === "POST" &&
      writable &&
      ["cancel", "resume", "apply", "discard"].includes(action)
    ) {
      if (!job.actions.includes(action)) {
        send(res, 409, { error: "Action unavailable" });
        return true;
      }
      if (action === "resume") {
        let text = "";
        for await (const chunk of req) text += chunk;
        let body;
        try {
          body = JSON.parse(text);
        } catch {
          send(res, 400, { error: "Invalid JSON" });
          return true;
        }
        if (!body.brief?.trim()) {
          send(res, 400, { error: "Instructions required" });
          return true;
        }
        const child = {
          ...structuredClone(job),
          id: `job-resumed-${++next}`,
          title: body.brief.split("\n")[0],
          status: "queued",
          createdAt: new Date().toISOString(),
          finishedAt: undefined,
          attention: [],
          worktree: undefined,
          resumeOf: job.id,
          resumedBy: [],
          actions: ["cancel"],
          inputTokens: 0,
          outputTokens: 0,
          activity: undefined,
        };
        job.resumedBy.push(child.id);
        job.actions = [];
        jobs.push(child);
        send(res, 200, { jobId: child.id });
      } else {
        if (action === "cancel") {
          job.status = "canceled";
          job.finishedAt = new Date().toISOString();
          job.actions = ["resume"];
        } else {
          job.worktree.status = action === "apply" ? "applied" : "discarded";
          job.attention = [];
          job.actions = action === "apply" ? ["discard"] : [];
        }
        send(res, 200, { ok: true });
      }
      return true;
    }
    if (req.method !== "GET") {
      send(res, 405, { error: "Read-only" });
      return true;
    }
    if (action === "events") {
      res.writeHead(200, {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-store",
      });
      // Real log.jsonl events carry no timestamp; keep fixtures faithful.
      const events = [
        { kind: "state", state: "turn_started" },
        {
          kind: "message",
          text: "Inspecting the existing panel and tests.",
          final: true,
        },
        {
          kind: "tool",
          tool: {
            id: "/read",
            phase: "start",
            name: "rg --files web/src",
            type: "shell",
          },
        },
        {
          kind: "tool",
          tool: {
            id: "/read",
            phase: "end",
            name: "rg --files web/src",
            type: "shell",
          },
        },
        {
          kind: "tool",
          tool: {
            id: "/edit",
            phase: "start",
            name: "apply_patch",
            type: "edit",
          },
        },
        {
          kind: "tool",
          tool: {
            id: "/edit",
            phase: "end",
            name: "apply_patch",
            type: "edit",
          },
        },
        { kind: "diff", diff: { files: 3, added: 42, deleted: 8 } },
        {
          kind: "usage",
          usage: { inputTokens: 8420, outputTokens: 1870 },
        },
        {
          kind: "system",
          state: "rate_limits",
          rateLimit: "7-day window: 35% used",
        },
        {
          kind: "tool",
          tool: {
            id: "/test",
            phase: "start",
            name: "go test ./internal/subpanel/...",
            type: "shell",
          },
        },
        {
          kind: "message",
          text: "Running the panel acceptance checks.",
          final: true,
        },
      ];
      if (job.status !== "running")
        events.push({
          kind: "state",
          state: job.status === "succeeded" ? "turn_completed" : "turn_failed",
        });
      for (const event of events)
        res.write(`data: ${JSON.stringify(event)}\n\n`);
      if (job.status === "running") {
        const heartbeat = setInterval(() => res.write(": heartbeat\n\n"), 1000);
        res.on("close", () => clearInterval(heartbeat));
      } else res.end("event: done\ndata: {}\n\n");
      return true;
    }
    if (action === "logs") {
      res.writeHead(200, {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-store",
      });
      res.end(
        'data: "tool exec go test ./internal/subpanel/..."\n\ndata: "message Tests passed."\n\nevent: done\ndata: {}\n\n',
      );
      return true;
    }
    if (action === "diff") {
      send(res, 200, {
        patch:
          "diff --git a/internal/subpanel/server.go b/internal/subpanel/server.go\n--- a/internal/subpanel/server.go\n+++ b/internal/subpanel/server.go\n@@ -0,0 +1 @@\n+func Listen(port int)\n",
        files: [],
        truncated: false,
        note: "",
      });
      return true;
    }
    send(res, 200, {
      job,
      brief: `# ${job.title}\n\nImplement and verify the task.`,
      markdown:
        "Completed the standalone read-only job panel.\nBackend APIs, live logs and diff checks passed.",
      result: {
        finalText: "Completed the task.",
        warnings: job.id === "job-failed" ? ["Review failed checks"] : [],
      },
    });
    return true;
  };
  handler.reset = () => {
    jobs = seedJobs();
    next = 0;
  };
  return handler;
}
