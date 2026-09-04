// shot-review-actions.mjs — visual gate for two T1 fixes:
//   1. ReviewGate's evidence well contrast (--color-text-3 3.39:1 → --color-text-2 6.80:1)
//   2. Review actions following task status (changes_requested → Accept/Changes greyed
//      out with a reason, instead of live buttons the server rejects)
//
// The Review column needs one awaiting_review card (live buttons) and one
// changes_requested card (disabled) side by side, which no seed state produces
// on demand — so the state API is intercepted with a fixture, the same technique
// board-gate-shot.mjs uses for the escalated lane.
//
// It spawns e2e/mock-server.mjs rather than pointing at the long-lived daemon:
// that server serves ../internal/serve/dist directly, so the shot is guaranteed
// to come from the COMMITTED build (shot-dispatch-review.mjs's contract).
//
// Usage: cd web && npm run build && node scripts/shot-review-actions.mjs
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { mkdir } from "node:fs/promises";
import { chromium } from "@playwright/test";

const __dirname = dirname(fileURLToPath(import.meta.url));
const WEB = join(__dirname, "..");
const OUTDIR = process.env.SHOT_OUT || "/tmp/pactify-shots";
const PORT = Number(process.env.SHOT_PORT || 4176);
const BASE = `http://localhost:${PORT}`;

const state = {
  project: "demo",
  awaiting_count: 1,
  agents: [
    { id: "claude-opus", roles: ["orchestrator", "reviewer"] },
    { id: "kimi", roles: ["worker"] },
  ],
  features: [
    {
      id: "add-2fa",
      branch: "feat/add-2fa",
      status: "active",
      tasks: [
        {
          id: "add-2fa-otp",
          owner: "kimi",
          reviewer: "claude-opus",
          status: "awaiting_review",
          spec: ".pact/tasks/add-2fa-otp.md",
          evidence: "go test ./... → ok 37 packages\nvitest 672/672 passed",
        },
        {
          id: "add-2fa-docs",
          owner: "kimi",
          reviewer: "claude-opus",
          status: "changes_requested",
          spec: ".pact/tasks/add-2fa-docs.md",
          evidence: "reviewer: needs a migration note before this can ship",
        },
      ],
    },
  ],
};

await mkdir(OUTDIR, { recursive: true });
const srv = spawn("node", ["e2e/mock-server.mjs"], {
  cwd: WEB,
  env: { ...process.env, PORT: String(PORT) },
  stdio: "ignore",
});
try {
  for (let i = 0; ; i++) {
    try {
      await fetch(BASE + "/api/projects");
      break;
    } catch {
      if (i > 50) throw new Error("mock server did not start");
      await new Promise((r) => setTimeout(r, 200));
    }
  }
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 960 }, deviceScaleFactor: 2 });
  await page.route("**/api/projects/*/state", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(state) }),
  );
  await page.goto(BASE, { waitUntil: "domcontentloaded" });
  await page.locator('[data-testid="app-root"]').waitFor();
  await page.getByTestId("lens-board").click().catch(() => {});
  await page.getByTestId("card-accept").first().waitFor();
  await page.waitForTimeout(500);
  await page.screenshot({ path: join(OUTDIR, "review-actions.png"), fullPage: true });

  // The evidence well lives in ui/ReviewGate, which the Dashboard lens mounts
  // per feature lane (Dashboard.tsx:496). NOTE: RunRail has its OWN panel using
  // the same data-testid="review-gate" — screenshot that one and you have not
  // seen this component at all.
  await page.getByTestId("lens-dashboard").click();
  const gate = page.getByTestId("review-gate").first();
  await gate.waitFor();
  await page.waitForTimeout(400);
  await gate.screenshot({ path: join(OUTDIR, "review-gate-evidence.png") });

  // Report what the buttons actually resolved to, so the gate has a machine
  // check next to the picture rather than only "it looked right".
  const rows = await page.evaluate(() => {
    const out = [];
    for (const b of document.querySelectorAll('[data-testid="card-accept"],[data-testid="card-changes"]')) {
      out.push({
        testid: b.getAttribute("data-testid"),
        disabled: b.disabled,
        title: b.getAttribute("title") || "",
      });
    }
    return out;
  });
  for (const r of rows) console.log(`${r.testid} disabled=${r.disabled} title=${JSON.stringify(r.title)}`);
  await browser.close();
  console.log(`shot: ${join(OUTDIR, "review-actions.png")}`);
} finally {
  srv.kill();
}
