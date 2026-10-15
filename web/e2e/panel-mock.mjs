// Standalone panel fixtures served from its final embedded build. This module
// also supplies a fresh ephemeral server to the screenshot script.
import http from "node:http";
import { readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { resolve, extname } from "node:path";

const dist = fileURLToPath(
  new URL("../../internal/subpanel/dist/", import.meta.url),
);
const base = {
  issue: "",
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
    issue: "AJ-659",
    status: "running",
    duration: "2m18s",
    inputTokens: 0,
    outputTokens: 0,
    activity: {
      summary: "tool exec go test ./internal/subpanel/...",
      inputTokens: 8420,
      outputTokens: 1870,
      usagePartial: true,
    },
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
    issue: "AJ-657",
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
    status: "queued",
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
];
export async function startPanelMock(port = 0) {
  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, "http://127.0.0.1");
    const json = (value) => {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify(value));
    };
    if (req.method !== "GET") {
      res.writeHead(405);
      res.end();
      return;
    }
    if (url.pathname === "/api/jobs") {
      json(panelJobs);
      return;
    }
    const match = url.pathname.match(
      /^\/api\/jobs\/([^/]+)(?:\/(logs|diff))?$/,
    );
    if (match) {
      const job = panelJobs.find(
        (job) => job.id === decodeURIComponent(match[1]),
      );
      if (!job) {
        res.writeHead(404);
        res.end();
        return;
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
        return;
      }
      if (match[2] === "diff") {
        json({
          patch:
            "diff --git a/internal/subpanel/server.go b/internal/subpanel/server.go\n--- a/internal/subpanel/server.go\n+++ b/internal/subpanel/server.go\n@@ -0,0 +1 @@\n+func Listen(port int)\n",
          files: [],
          truncated: false,
          note: "",
        });
        return;
      }
      json({
        job,
        brief:
          "# AJ-659: Local job panel\n\nA standalone read-only panel for job summaries, results, logs and diffs.\n\nAcceptance criteria\n- Listen only on 127.0.0.1; protect APIs with a temporary token\n- Reuse JobSummary and link resumed jobs\n- Support dark mode and mobile widths",
        result: { finalText: "Completed the standalone read-only job panel." },
        markdown:
          "✓ codex succeeded\n\nHarness: codex (minimal config)\nModel: gpt-5.4 → gpt-5.4\nTokens: in 12480 · out 3260\nChanges: 6 files · +218/−12\n\nFinal reply:\nCompleted the standalone read-only job panel.\nBackend APIs, live logs and diff checks passed.",
      });
      return;
    }
    const path = resolve(
      dist,
      "." + (url.pathname === "/" ? "/index.html" : url.pathname),
    );
    if (!path.startsWith(dist)) {
      res.writeHead(404);
      res.end();
      return;
    }
    try {
      const data = await readFile(path);
      const type =
        {
          ".html": "text/html",
          ".js": "text/javascript",
          ".css": "text/css",
          ".woff2": "font/woff2",
          ".svg": "image/svg+xml",
        }[extname(path)] || "application/octet-stream";
      res.writeHead(200, { "Content-Type": type });
      res.end(data);
    } catch {
      res.writeHead(404);
      res.end();
    }
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", resolve);
  });
  return { server, baseURL: `http://127.0.0.1:${server.address().port}` };
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const { server, baseURL } = await startPanelMock(
    Number(process.env.PANEL_E2E_PORT || 4176),
  );
  console.log(baseURL);
  for (const signal of ["SIGTERM", "SIGINT"])
    process.on(signal, () => {
      server.closeAllConnections();
      server.close();
    });
}
