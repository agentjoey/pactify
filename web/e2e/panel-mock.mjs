// Standalone panel fixtures served from its final embedded build. This module
// also supplies a fresh ephemeral server to the screenshot script.
import http from "node:http";
import { readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { resolve, extname } from "node:path";

const dist = fileURLToPath(
  new URL("../../internal/subpanel/dist/", import.meta.url),
);
export { panelJobs } from "./jobs-fixtures.mjs";
import { createRouterAPI } from "./router-fixtures.mjs";
import { serveJobsAPI } from "./jobs-fixtures.mjs";

export async function startPanelMock(port = 0) {
  const routerAPI = createRouterAPI();
  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, "http://127.0.0.1");
    if (req.method !== "GET") {
      res.writeHead(405);
      res.end();
      return;
    }
    if (await routerAPI(req, res, url.pathname)) return;
    if (serveJobsAPI(req, res, url.pathname)) return;
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
