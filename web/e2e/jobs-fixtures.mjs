// One set of job API fixtures for the dashboard and standalone panel.
const base = {
  harness: "codex",
  modelRequested: "gpt-5.4",
  modelActual: "gpt-5.4",
  repo: "/workspace/pactify",
  cwd: "/workspace/pactify",
  access: "write",
  configMode: "minimal",
  status: "succeeded",
  duration: "3m42s",
  createdAt: "2026-10-16T01:00:00Z",
  inputTokens: 12480,
  outputTokens: 3260,
  attention: [],
  resumeOf: "",
  resumedBy: [],
};
export const panelJobs = [
  {
    ...base,
    id: "job-panel",
    title: "AJ-659 · Local read-only job panel",
    status: "succeeded",
    duration: "2m18s",
    inputTokens: 8420,
    outputTokens: 1870,
    usagePartial: true,
    attention: [{kind: "worktree_pending", message: "Worktree is awaiting apply or discard"}],
    worktree: {
      path: "/tmp/pactify/jobs/job-panel/wt",
      baseCommit: "97e9d6bf2143",
      status: "pending",
    },
    resumedBy: ["job-review"],
  },
  {
    ...base,
    id: "job-summary",
    title: "AJ-657 · Shared job summary model",
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
  },
  {
    ...base,
    id: "job-kimi",
    title: "Evaluate resume for large tasks",
    harness: "kimi",
    modelActual: "kimi-k2.5",
    modelRequested: "kimi-k2.5",
    repo: "/workspace/harness-eval",
    duration: "5m09s",
    inputTokens: 26340,
    outputTokens: 4120,
    createdAt: "2026-10-16T00:50:00Z",
  },
  {
    ...base,
    id: "job-review",
    title: "Panel acceptance and visual checks",
    status: "running",
    activity: {summary: "tool exec go test ./internal/subpanel/...", inputTokens: 8420, outputTokens: 1870, usagePartial: true},
    worktree: {path: "/tmp/pactify/jobs/job-panel/wt", baseCommit: "97e9d6bf2143", status: "pending"},
    resumeOf: "job-panel",
    duration: "12s",
    inputTokens: 0,
    outputTokens: 0,
    createdAt: "2026-10-16T01:10:00Z",
  },
  {
    ...base,
    id: "job-failed",
    title: "Fix CLI exit code edge cases",
    status: "failed",
    duration: "48s",
    createdAt: "2026-10-15T23:00:00Z",
    attention: [{ kind: "failed", message: "Job failed" }],
    inputTokens: 2160,
    outputTokens: 360,
  },
  {
    ...base,
    id: "job-config",
    title: "Isolate harness config and sessions",
    repo: "/workspace/harness-eval",
    access: "read-only",
    createdAt: "2026-10-15T22:00:00Z",
    duration: "1m56s",
  },
  {
    ...base, id: "job-quota", title: "Recover a quota-limited task", status: "failed",
    failureClass: "quota", attention: [{kind: "failed", message: "Job failed"}],
    fallbackTo: "job-fallback", createdAt: "2026-10-15T21:00:00Z",
  },
  {
    ...base, id: "job-fallback", title: "Fallback attempt", harness: "opencode",
    modelRequested: "deepseek-flash", modelActual: "deepseek-flash", fallbackFrom: "job-quota",
    createdAt: "2026-10-15T21:01:00Z",
  },
  {
    ...base, id: "job-canceled", title: "Canceled experiment", status: "canceled",
    createdAt: "2026-10-15T20:00:00Z",
  },
];
export function serveJobsAPI(req, res, pathname) {
  if (pathname !== "/api/cost" && pathname !== "/api/jobs" && !pathname.startsWith("/api/jobs/")) return false;
  if (req.method !== "GET") {
    res.writeHead(405, { Allow: "GET" });
    res.end();
    return true;
  }
  const json = (value) => {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify(value));
  };
  if (pathname === "/api/cost") {
    const now = new Date();
    const since = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 6);
    const empty = {
      jobs: 0, succeeded: 0, failed: 0, canceled: 0, retries: 0, fallbacks: 0,
      inputTokens: 0, outputTokens: 0, costUSD: null, costReportedJobs: 0,
      subscriptionJobs: 0, unpricedJobs: 0, usagePartialJobs: 0,
    };
    json({
      since: since.toISOString(), until: now.toISOString(), by: "harness",
      groups: [
        { ...empty, key: "codex", jobs: 2, succeeded: 1, failed: 1, inputTokens: 12345, outputTokens: 2345, subscriptionJobs: 2, usagePartialJobs: 1 },
        { ...empty, key: "custom", jobs: 1, failed: 1, inputTokens: 50, outputTokens: 10, unpricedJobs: 1 },
        { ...empty, key: "opencode", jobs: 1, succeeded: 1, inputTokens: 2000, outputTokens: 300, costUSD: 0.125, costReportedJobs: 1 },
      ],
      total: { ...empty, jobs: 4, succeeded: 2, failed: 2, inputTokens: 14395, outputTokens: 2655, costUSD: 0.125, costReportedJobs: 1, subscriptionJobs: 2, unpricedJobs: 1, usagePartialJobs: 1 },
      skipped: { missingResult: 0, invalidResult: 0, invalidState: 0 },
      quotaSnapshots: [],
      exhaustedRoutes: [{ harness: "codex", model: "gpt-5", resetAt: new Date(now.getTime() + 150 * 60000).toISOString(), recordedAt: now.toISOString(), jobId: "job-failed", message: "Quota exhausted" }],
    });
    return true;
  }
  if (pathname === "/api/jobs") {
    json(panelJobs);
    return true;
  }
  const match = pathname.match(
    /^\/api\/jobs\/([^/]+)(?:\/(logs|diff))?$/,
  );
  if (match) {
    const job = panelJobs.find(
      (job) => job.id === decodeURIComponent(match[1]),
    );
    if (!job) {
      res.writeHead(404);
      res.end();
      return true;
    }
    if (match[2] === "logs") {
      res.writeHead(200, {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-cache",
      });
      res.write(
        `data: ${JSON.stringify("tool exec go test ./internal/subpanel/...")}\n\n`,
      );
      const timer = setTimeout(() => {
        res.write(`data: ${JSON.stringify("message Tests passed.")}\n\n`);
        res.end("event: done\ndata: {}\n\n");
      }, 120);
      req.on("close", () => clearTimeout(timer));
      return true;
    }
    if (match[2] === "diff") {
      json({
        patch:
          "diff --git a/internal/subpanel/server.go b/internal/subpanel/server.go\n--- a/internal/subpanel/server.go\n+++ b/internal/subpanel/server.go\n@@ -0,0 +1 @@\n+func Listen(port int)\n",
        files: [],
        truncated: false,
        note: "",
      });
      return true;
    }
    json({
      job,
      brief:
        "# AJ-659: Local job panel\n\nA standalone read-only panel for job summaries, results, logs and diffs.\n\nAcceptance criteria\n- Listen only on 127.0.0.1; protect APIs with a temporary token\n- Reuse JobSummary and link resumed jobs\n- Support dark mode and mobile widths",
      result: { finalText: "Completed the standalone read-only job panel." },
      markdown:
        "✓ codex succeeded\n\nHarness: codex (minimal config)\nModel: gpt-5.4 → gpt-5.4\nTokens: in 12480 · out 3260\nChanges: 6 files · +218/−12\n\nFinal reply:\nCompleted the standalone read-only job panel.\nBackend APIs, live logs and diff checks passed.",
    });
    return true;
  }
  res.writeHead(404);
  res.end();
  return true;
}
