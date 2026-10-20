import { createServer as createHTTPServer } from "node:http";
import type { AddressInfo } from "node:net";
import { resolve } from "node:path";
import { createServer } from "vite";
import { expect, it, vi } from "vitest";

it("dev proxy forwards writes with the backend Host and exact Origin", async () => {
  const upstream = createHTTPServer((req, res) => {
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify({ host: req.headers.host, origin: req.headers.origin }));
  });
  await new Promise<void>((done) => upstream.listen(0, "127.0.0.1", done));
  const target = `http://127.0.0.1:${(upstream.address() as AddressInfo).port}`;
  vi.stubEnv("PACTIFY_SERVE_URL", target);
  const vite = await createServer({
    root: resolve(import.meta.dirname, ".."),
    server: { host: "127.0.0.1", port: 0, hmr: false },
    optimizeDeps: { noDiscovery: true, include: [] },
  });
  try {
    await vite.listen();
    const url = `http://127.0.0.1:${(vite.httpServer!.address() as AddressInfo).port}/api/sub/router`;
    for (const method of ["POST", "PUT", "PATCH", "DELETE"]) {
      const result = await fetch(url, { method, headers: { Origin: "http://localhost:5173" } });
      expect(await result.json()).toEqual({ host: new URL(target).host, origin: target });
    }
    const cli = await fetch(url, { method: "PUT" });
    expect(await cli.json()).toEqual({ host: new URL(target).host });
  } finally {
    await vite.close();
    await new Promise<void>((done) => upstream.close(() => done()));
    vi.unstubAllEnvs();
  }
}, 20000);
