// Serve only the final embedded build and the harness APIs.
import http from "node:http";
import { readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { resolve, extname, sep } from "node:path";
import { createJobsAPI } from "./jobs-fixtures.mjs";
import { createRouterAPI } from "./router-fixtures.mjs";
export async function startMock(port = 0, panel = false) {
  const dist = fileURLToPath(
    new URL(
      panel ? "../../internal/subpanel/dist/" : "../../internal/serve/dist/",
      import.meta.url,
    ),
  );
  const jobs = createJobsAPI(!panel),
    router = createRouterAPI(!panel);
  const server = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url, "http://localhost");
      if (req.method === "POST" && url.pathname === "/__test/reset") {
        jobs.reset();
        router.reset();
        res.writeHead(200);
        res.end("{}");
        return;
      }
      const path = panel
        ? url.pathname
        : url.pathname.replace(/^\/api\/sub(?=\/)/, "/api");
      if ((await jobs(req, res, path)) || (await router(req, res, path)))
        return;
      if (url.pathname.startsWith("/api/")) {
        res.writeHead(404);
        res.end();
        return;
      }
      const file = resolve(
        dist,
        "." + (url.pathname === "/" ? "/index.html" : url.pathname),
      );
      if (!file.startsWith(dist.endsWith(sep) ? dist : dist + sep)) {
        res.writeHead(404);
        res.end();
        return;
      }
      const data = await readFile(file);
      res.writeHead(200, {
        "Content-Type":
          {
            ".html": "text/html",
            ".js": "text/javascript",
            ".css": "text/css",
            ".woff2": "font/woff2",
            ".svg": "image/svg+xml",
          }[extname(file)] || "application/octet-stream",
        "Cache-Control": "no-store",
      });
      res.end(data);
    } catch {
      res.writeHead(500);
      res.end("Mock request failed");
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
  const { server, baseURL } = await startMock(Number(process.env.PORT || 4173));
  console.log(baseURL);
  for (const signal of ["SIGTERM", "SIGINT"])
    process.on(signal, () => {
      server.closeAllConnections();
      server.close();
    });
}
