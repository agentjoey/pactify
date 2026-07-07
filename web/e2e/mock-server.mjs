// Hermetic mock server for the Playwright acceptance gate (spec §4).
//
// Serves the built SPA (../internal/serve/dist) plus the subset of the real
// pactify HTTP API the dashboard reads on boot, with NO Go backend. Shapes match
// internal/serve exactly (see fixtures.mjs for the field-by-field alignment):
//   GET  /api/projects              → [{id,name,path,project,feature_count,awaiting_count,group?}]
//   GET  /api/registry              → [{name,path,group?,status}]
//   GET  /api/acting-seat           → {seat}            (author identity)
//   GET  /api/projects/p1/state     → StateDTO          (mutable working copy)
//   GET  /api/projects/p1/events    → SSE stream ("event: pact\ndata: …\n\n")
//   GET  /api/fs/browse?path=       → {path,parent,entries}
//   GET  /api/setup/suggest         → {bindings,warnings}
//   POST /api/setup/apply           → {inited,wired,notes}
//   POST /api/registry              → {name}
//   DELETE /api/registry/:name      → {name}
//   (unhandled /api/* → 404; everything else → SPA fallback to index.html)
//
// Test hooks (NOT part of the real API):
//   POST /__test/reset     → restore the seed state (per-test).
//
// Draft state is NOT server-side — drafts are browser-local; the connect/author
// tests create them through the UI.

import { createServer } from "node:http";
import { readFile, stat } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join, normalize, extname } from "node:path";
import {
  PROJECT_ID,
  ACTING_SEAT,
  registry as makeRegistry,
  projects as makeProjects,
  initialState,
  browseTree,
  setupSuggest,
} from "./fixtures.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));
const DIST = join(__dirname, "..", "..", "internal", "serve", "dist");
const PORT = Number(process.env.PORT || 4173);

// --- mutable per-process state (reset via /__test/reset between tests) --------
let state = initialState();
const sseClients = new Set(); // live SSE response objects
let registry = makeRegistry();
const fsTree = browseTree();

function resetState() {
  state = initialState();
  registry = makeRegistry();
}

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".svg": "image/svg+xml",
  ".json": "application/json; charset=utf-8",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
  ".png": "image/png",
  ".ico": "image/x-icon",
};

function sendJSON(res, code, body) {
  const s = JSON.stringify(body);
  res.writeHead(code, { "Content-Type": "application/json; charset=utf-8" });
  res.end(s);
}

async function readBody(req) {
  const chunks = [];
  for await (const c of req) chunks.push(c);
  return Buffer.concat(chunks).toString("utf8");
}

// pushEvent fans a single SSE "pact" frame out to every connected client. The
// dashboard's subscribeEvents listens for the "pact" event and refetches state,
// so any payload triggers the refetch — we send a minimal well-formed PactEvent.
function pushEvent(ev) {
  const line = JSON.stringify(ev);
  for (const res of sseClients) {
    // A client may have disconnected without its 'close' firing yet (or be
    // mid-teardown); writing to a finished socket throws ERR_STREAM_WRITE_AFTER_END
    // and would crash the whole fan-out. Guard each write and evict the dead client.
    try {
      res.write(`event: pact\ndata: ${line}\n\n`);
    } catch {
      sseClients.delete(res);
    }
  }
}

// --- static SPA serving (dist + index.html fallback) --------------------------
async function serveStatic(req, res) {
  // Map the URL path onto dist, guarding against traversal. Unknown non-asset
  // paths fall back to index.html (SPA routing).
  const urlPath = decodeURIComponent((req.url || "/").split("?")[0]);
  const rel = normalize(urlPath).replace(/^(\.\.[/\\])+/, "");
  let filePath = join(DIST, rel);

  let info = null;
  try {
    info = await stat(filePath);
  } catch {
    info = null;
  }
  if (info && info.isDirectory()) {
    filePath = join(filePath, "index.html");
    info = await stat(filePath).catch(() => null);
  }
  if (!info) {
    // SPA fallback — only for non-/api paths (api 404s handled earlier).
    filePath = join(DIST, "index.html");
  }
  try {
    const buf = await readFile(filePath);
    res.writeHead(200, { "Content-Type": MIME[extname(filePath)] || "application/octet-stream" });
    res.end(buf);
  } catch {
    res.writeHead(404, { "Content-Type": "text/plain" });
    res.end("not found");
  }
}

const server = createServer(async (req, res) => {
  const { method } = req;
  const url = (req.url || "/").split("?")[0];

  // --- test hooks ---
  if (url === "/__test/reset" && method === "POST") {
    resetState();
    return sendJSON(res, 200, { status: "ok" });
  }

  // --- real API surface ---
  if (url === "/api/projects" && method === "GET") {
    return sendJSON(res, 200, makeProjects(registry));
  }
  if (url === "/api/registry" && method === "GET") {
    return sendJSON(res, 200, registry);
  }
  if (url === "/api/acting-seat" && method === "GET") {
    return sendJSON(res, 200, { seat: ACTING_SEAT });
  }
  if (url === `/api/projects/${PROJECT_ID}/state` && method === "GET") {
    return sendJSON(res, 200, state);
  }
  if (url === `/api/projects/${PROJECT_ID}/events` && method === "GET") {
    res.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      Connection: "keep-alive",
    });
    res.write(": connected\n\n"); // comment line / initial flush
    sseClients.add(res);
    // Heartbeat comment lines keep the connection warm (mirrors a long-lived
    // serve stream); they are ignored by EventSource.
    const hb = setInterval(() => {
      res.write(": heartbeat\n\n");
    }, 15000);
    req.on("close", () => {
      clearInterval(hb);
      sseClients.delete(res);
    });
    return;
  }

  if (url === "/api/fs/browse" && method === "GET") {
    const pathParam = decodeURIComponent((req.url || "").split("?path=")[1] || "");
    const node = fsTree[pathParam || "/tmp"];
    if (!node) return sendJSON(res, 400, { error: "path does not exist or is not a directory" });
    return sendJSON(res, 200, node);
  }

  if (url === "/api/setup/suggest" && method === "GET") {
    return sendJSON(res, 200, setupSuggest());
  }

  if (url === "/api/setup/apply" && method === "POST") {
    const body = JSON.parse(await readBody(req));
    const name = body.project || "unknown";
    registry.push({ name, path: body.path, group: body.group || "", status: { valid: true, seats: (body.seats || []).length } });
    return sendJSON(res, 200, { inited: true, wired: [], notes: [] });
  }

  if (url === "/api/registry" && method === "POST") {
    const body = JSON.parse(await readBody(req));
    const name = body.name || body.path.split("/").filter(Boolean).pop() || "project";
    registry.push({ name, path: body.path, group: body.group || "", status: { valid: true, seats: 0 } });
    return sendJSON(res, 200, { name });
  }

  if (url.startsWith("/api/registry/") && method === "DELETE") {
    const name = decodeURIComponent(url.slice("/api/registry/".length));
    registry = registry.filter((p) => p.name !== name);
    return sendJSON(res, 200, { name });
  }

  // --- DispatchPanel plan generation + apply mocks ---
  if (url === `/api/projects/${PROJECT_ID}/plan/generate` && method === "POST") {
    return sendJSON(res, 202, { status_url: "/x", feature: "add-2fa" });
  }
  if (url === `/api/projects/${PROJECT_ID}/plan/generate/status` && method === "GET") {
    return sendJSON(res, 200, { state: "done", feature: "add-2fa" });
  }
  if (url === `/api/projects/${PROJECT_ID}/plan/add-2fa` && method === "GET") {
    return sendJSON(res, 200, {
      present: true,
      feature: "add-2fa",
      branch: "feat-2fa",
      valid: true,
      tasks: [
        {
          id: "add-2fa-otp",
          owner: "kimi",
          reviewer: "claude-opus",
          spec: ".pact/tasks/x.md",
          verify: "go test ./...",
        },
      ],
    });
  }
  if (url === `/api/projects/${PROJECT_ID}/plan/add-2fa/apply` && method === "POST") {
    return sendJSON(res, 200, { assigned: 1 });
  }
  if (url === `/api/projects/${PROJECT_ID}/orchestrate/run` && method === "POST") {
    return sendJSON(res, 202, { status_url: "/x" });
  }

  // Unhandled /api/* → 404 (do NOT fall back to the SPA for API paths).
  if (url.startsWith("/api/")) {
    return sendJSON(res, 404, { error: "not found" });
  }

  // Everything else → static SPA.
  return serveStatic(req, res);
});

server.listen(PORT, () => {
  console.log(`[mock-server] listening on http://localhost:${PORT} (dist: ${DIST})`);
});
