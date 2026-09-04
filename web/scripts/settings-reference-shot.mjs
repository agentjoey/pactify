import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";

const OUT = process.env.SHOT_OUT || "/tmp/pactify-shots";
const REF =
  process.env.REF_PATH ||
  join(homedir(), "AgentWorks/Code_Claude/design_handoff_dark_product_ui/designs/Pactify Settings.dc.html");

await mkdir(OUT, { recursive: true });
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1200, height: 1000 }, deviceScaleFactor: 2 });
await p.goto(`file://${REF}`, { waitUntil: "domcontentloaded" });
await p.waitForTimeout(500);
await p.screenshot({ path: `${OUT}/settings-reference.png` });
console.log("settings reference shot");
await b.close();
