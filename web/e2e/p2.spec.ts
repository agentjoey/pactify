import { test, expect } from "@playwright/test";

for (const panel of [true, false]) test.describe(panel ? "panel P2" : "dashboard P2", () => {
  test.use({baseURL: panel ? `http://127.0.0.1:${process.env.PANEL_E2E_PORT || 4176}` : `http://localhost:${process.env.E2E_PORT || 4173}`});
  test.beforeEach(async ({request}) => {await request.post("/__test/reset");});
  for (const width of [1440,390]) {
    test(`shortcuts, focus restoration and help at ${width}px`, async ({page}) => {
      await page.setViewportSize({width,height:844});
      const writes:string[]=[];
      page.on("request", request => {if (request.url().includes("/api/") && request.method() !== "GET") writes.push(request.url());});
      await page.goto("/#/jobs");
      const search=page.getByRole("searchbox");
      await expect(search).toBeVisible();
      await page.keyboard.press("/"); await expect(search).toBeFocused();
      await page.keyboard.type("jk/?"); await expect(search).toHaveValue("jk/?");
      await expect(page.getByRole("region",{name:"Keyboard shortcuts",exact:true})).toHaveCount(0);
      await search.fill(""); await page.keyboard.press("Tab");
      const first=page.getByRole("region",{name:"In progress",exact:true}).getByRole("link");
      const second=page.getByRole("region",{name:"Queued",exact:true}).getByRole("link").first();
      await page.keyboard.press("j"); await expect(first).toBeFocused();
      expect(await first.evaluate(node => getComputedStyle(node).outlineStyle)).toBe("solid");
      await page.keyboard.press("j"); await expect(second).toBeFocused();
      await page.keyboard.press("k"); await expect(first).toBeFocused();
      await page.keyboard.press("Enter");
      const details=page.getByRole("dialog",{name:"Job details"});
      await expect(details).toBeVisible();
      await page.keyboard.press("/"); await expect(search).not.toBeFocused();
      await page.keyboard.press("Escape"); await expect(details).toHaveCount(0); await expect(first).toBeFocused();
      await page.keyboard.press("?");
      const help=page.getByRole("region",{name:"Keyboard shortcuts",exact:true});
      await expect(help).toBeVisible();
      const box=await help.boundingBox();
      expect(box!.x).toBeGreaterThanOrEqual(0); expect(box!.x+box!.width).toBeLessThanOrEqual(width);
      await test.info().attach(`shortcuts-${width}`,{body:await page.screenshot({fullPage:true}),contentType:"image/png"});
      await page.keyboard.press("Escape"); await expect(help).toHaveCount(0);
      if (panel) expect(writes).toEqual([]);
    });
  }
  test("mobile KPI rows contain two and three tiles and serve a bundled status icon", async ({page,request}) => {
    await page.setViewportSize({width:390,height:844});
    await page.goto("/#/dashboard");
    await expect(page.locator(".kpi")).toHaveCount(5);
    const boxes=await page.locator(".kpi").evaluateAll(nodes=>nodes.map(node=>{const {x,y}=node.getBoundingClientRect();return {x,y};}));
    expect(boxes[0].y).toBe(boxes[1].y);
    expect(boxes[2].y).toBeGreaterThan(boxes[0].y);
    expect(boxes[2].y).toBe(boxes[3].y); expect(boxes[3].y).toBe(boxes[4].y);
    expect(boxes[2].x).toBeLessThan(boxes[3].x); expect(boxes[3].x).toBeLessThan(boxes[4].x);
    await expect(page.locator('link[rel="icon"]')).toHaveAttribute("href","/favicon-attention.svg");
    for (const state of ["neutral","running","attention"]) {
      const icon=await request.get(`/favicon-${state}.svg`); expect(icon.ok()).toBe(true); expect(icon.headers()["content-type"]).toContain("image/svg+xml");
    }
    await page.evaluate(()=>document.fonts.ready);
    expect(await page.evaluate(()=>document.fonts.check('13px InterVariable', 'Café Ā Ł Œ → · −'))).toBe(true);
    expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1);
    await test.info().attach("mobile-kpis",{body:await page.screenshot({fullPage:true}),contentType:"image/png"});
  });
});
