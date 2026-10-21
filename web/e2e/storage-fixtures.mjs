export function createStorageAPI(writable) {
  const plan = {
    version: "storage-fixture-v1", computedAt: "2026-10-21T00:00:00Z",
    policy: { olderThan: "336h", cacheMaxBytes: 5000000000, interval: "24h" },
    locations: [
      { name: "jobs", bytes: 1900000000, reclaimable: 380000000, items: [
        { path: "jobs/old-completed", bytes: 380000000, protected: false, reason: "finished resume chain older than retention" },
        { path: "jobs/stale-worktree", bytes: 750000000, protected: true, reason: "pending worktree; explicit --include-worktrees required" },
      ] },
      { name: "cache", bytes: 4800000000, reclaimable: 0, breakdown: { "go-build": 4100000000, npm: 548000000, "go-mod": 177000000 }, items: [] },
      { name: "harness-homes", bytes: 120700000, reclaimable: 7000000, breakdown: { opencode: 78000000, agy: 41000000, kimi: 1700000 }, items: [
        { path: "harness-homes/agy/logs/old.log", bytes: 7000000, protected: false, reason: "harness log older than retention" },
        { path: "harness-homes/kimi/credentials", bytes: 0, protected: true, reason: "credential protected" },
      ] },
    ], staleWorktrees: ["job-stale-worktree"], failures: [],
  };
  let lastRun = null;
  async function handler(req, res, path) {
    if (!["/api/storage", "/api/storage/plan", "/api/storage/prune"].includes(path)) return false;
    let code = 200, value = path.endsWith("/plan") ? plan : { ...plan, lastRun };
    if (req.method === "POST" && writable && path.endsWith("/prune")) {
      let body = ""; for await (const chunk of req) body += chunk;
      if (JSON.parse(body).version !== plan.version) { code = 409; value = { error: "storage changed; review a new cleanup plan" }; }
      else value = lastRun = { startedAt: "2026-10-21T01:00:00Z", endedAt: "2026-10-21T01:00:01Z", removed: [{ location: "jobs", items: 1, bytes: 380000000 }, { location: "cache", items: 0, bytes: 0 }, { location: "harness-homes", items: 1, bytes: 7000000 }], failures: [] };
    } else if (req.method !== "GET" || path.endsWith("/prune")) { code = 405; value = { error: "GET required" }; }
    res.writeHead(code, { "Content-Type": "application/json" }); res.end(JSON.stringify(value)); return true;
  }
  handler.reset = () => { lastRun = null; };
  return handler;
}
