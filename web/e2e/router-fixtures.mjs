import { createHash } from "node:crypto";

// Each host gets its own in-memory router; only the dashboard enables writes.
export function createRouterAPI(writable = false) {
  let config, data, quotas;
  function reset() {
    config = {
      aliases: { fast: { harness: "codex", model: "gpt-test", effort: "low" } },
      fallback: { codex: ["@fast", "kimi"] },
      quotaGroups: { shared: { harness: "agy", models: ["one", "two"] } },
    };
    data = JSON.stringify(config);
    quotas = [
      {
        key: "codex/gpt-test",
        harness: "codex",
        model: "gpt-test",
        resetAt: new Date(Date.now() + 6 * 3600000).toISOString(),
        recordedAt: new Date().toISOString(),
        jobId: "job-panel",
        message: "Usage limit",
      },
    ];
  }
  reset();
  const version = () => createHash("sha256").update(data).digest("hex");
  function send(res, status, body) {
    res.writeHead(status, {
      "Content-Type": "application/json",
      "Cache-Control": "no-store",
    });
    res.end(JSON.stringify(body));
  }
  async function handler(req, res, path) {
    if (path !== "/api/router" && !path.startsWith("/api/quota/")) return false;
    if (req.method === "GET" && path === "/api/router") {
      send(res, 200, {
        path: "/tmp/pactify/router.json",
        exists: true,
        version: version(),
        config,
        exhaustedRoutes: quotas,
      });
    } else if (writable && req.method === "PUT" && path === "/api/router") {
      if (
        !/^application\/json(?:;|$)/i.test(req.headers["content-type"] || "")
      ) {
        send(res, 415, { error: "application/json required" });
        return true;
      }
      let body = "";
      for await (const chunk of req) body += chunk;
      if (Buffer.byteLength(body) > 65536) {
        send(res, 413, { error: "body exceeds 64 KiB" });
        return true;
      }
      let request;
      try {
        request = JSON.parse(body);
      } catch {
        send(res, 400, { error: "invalid JSON body" });
        return true;
      }
      if (request.version !== version())
        send(res, 409, {
          error: "Config changed elsewhere — reload to see the latest",
        });
      else if (
        !request.config?.aliases ||
        !request.config?.fallback ||
        !request.config?.quotaGroups
      )
        send(res, 422, {
          errors: [{ field: "config", message: "Invalid routing config" }],
        });
      else {
        config = request.config;
        data = JSON.stringify(config);
        send(res, 200, { saved: true });
      }
    } else if (
      writable &&
      req.method === "DELETE" &&
      path.startsWith("/api/quota/")
    ) {
      const key = decodeURIComponent(path.slice("/api/quota/".length));
      quotas = quotas.filter((q) => q.key !== key);
      send(res, 200, { cleared: true });
    } else send(res, 405, { error: "method not allowed" });
    return true;
  }
  handler.reset = reset;
  return handler;
}
