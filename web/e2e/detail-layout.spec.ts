import { expect, test, type Locator } from "@playwright/test";

async function expectInside(outer: Locator, inner: Locator) {
  const area = await outer.boundingBox();
  const box = await inner.boundingBox();
  expect(area).not.toBeNull();
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(area!.x);
  expect(box!.y).toBeGreaterThanOrEqual(area!.y);
  expect(box!.x + box!.width).toBeLessThanOrEqual(area!.x + area!.width);
  expect(box!.y + box!.height, `${inner} bottom must be inside ${outer}`).toBeLessThanOrEqual(area!.y + area!.height);
}

for (const panel of [false, true]) {
  for (const [width, height] of [[1440, 900], [390, 844]]) {
    test(`${panel ? "panel" : "dashboard"} detail keeps its footer visible at ${width}x${height}`, async ({page}) => {
      await page.setViewportSize({width, height});
      const base = panel ? `http://127.0.0.1:${process.env.PANEL_E2E_PORT || 4176}` : "";
      const api = panel ? "/api/jobs" : "/api/sub/jobs";
      await page.route(`**${api}/job-summary`, async route => {
        const response = await route.fetch();
        const detail = await response.json();
        detail.markdown += "\n" + "A recorded result line.\n".repeat(100);
        await route.fulfill({json:detail});
      });
      await page.goto(`${base}/#/jobs/job-summary?tab=summary`);
      const modal = page.getByRole("dialog", {name:"Job details"});
      await expect(modal.getByRole("heading", {name:"Result summary",exact:true})).toBeVisible();
      const footer = modal.locator(".detail-footer");
      const controls = panel
        ? [footer.getByRole("heading", {name:"Next steps",exact:true}),
           ...(width > 700 ? await footer.getByRole("button", {name:/^Copy pactify sub /}).all()
             : [footer.getByRole("button", {name:/^(Show|Hide) commands$/})])]
        : ["Resume", "Apply", "Discard"].map(name => footer.getByRole("button", {name,exact:true}));
      // Check before scrolling: an offscreen footer must fail even if scrolling
      // to the bottom would eventually reveal its controls.
      for (const control of controls) await expectInside(modal, control);
      await expectInside(modal, footer);
      const initial = await footer.boundingBox();
      const scroller = modal.locator(".detail-scroll");
      for (const position of [0.5, 1, 0]) {
        await scroller.evaluate((node, fraction) => {node.scrollTop = (node.scrollHeight - node.clientHeight) * fraction;}, position);
        for (const control of controls) await expectInside(modal, control);
        const box = await footer.boundingBox();
        expect(Math.abs(box!.y - initial!.y)).toBeLessThanOrEqual(1);
      }
      if (panel) {
        if (width <= 700) await footer.getByRole("button", {name:"Show commands",exact:true}).click();
        else await expect(footer.getByRole("button", {name:"Show commands",exact:true})).toHaveCount(0);
        const commands = modal.getByRole("region", {name:width <= 700 ? "Terminal commands" : "Next steps",exact:true});
        await expect(modal.locator(".next-steps")).toHaveCount(1);
        await expect(commands.getByRole("heading")).toBeInViewport();
        await expect(commands.getByRole("button", {name:/^Copy pactify sub /})).toHaveCount(5);
        const last = commands.getByRole("button", {name:"Copy pactify sub result job-summary",exact:true});
        await last.scrollIntoViewIfNeeded();
        await expectInside(modal, last);
        await expectInside(modal, controls[0]);
      }
      await modal.getByRole("tab", {name:"Live",exact:true}).click();
      await expect(modal.getByText("3 observed calls", {exact:true})).toBeVisible();
      const tools = modal.locator(".live-tools");
      await tools.scrollIntoViewIfNeeded();
      await expectInside(scroller, tools);
      const row = await tools.boundingBox();
      const bottom = await footer.boundingBox();
      expect(row!.y + row!.height).toBeLessThanOrEqual(bottom!.y);
      for (const control of controls) await expectInside(modal, control);
    });
  }
}
