import { expect, test } from "@playwright/test";
for (const panel of [false,true]) test.describe(panel ? "panel pipeline" : "dashboard pipeline",()=>{
 test.use({baseURL: panel ? `http://127.0.0.1:${process.env.PANEL_E2E_PORT || 4176}` : `http://localhost:${process.env.E2E_PORT || 4173}`});
 test.beforeEach(async ({request})=>{await request.post('/__test/reset');});
test("Running snapping clears the sticky line rail",async({page})=>{
  await page.setViewportSize({width:1100,height:900});await page.goto('/#/jobs');
  const board=page.locator('.pipeline-scroll');await expect(board).toBeVisible();
  await board.evaluate(el=>{el.scrollLeft=el.scrollWidth;});
  await board.focus();
  await page.keyboard.press('h');
  const rail=await page.locator('.line-rail').boundingBox();
  const header=await page.locator('[data-station="progress"] .station-heading').boundingBox();
  expect(header!.x).toBeGreaterThanOrEqual(rail!.x+rail!.width-1);
  const viewport=await board.boundingBox();
  expect(header!.x+header!.width).toBeLessThanOrEqual(viewport!.x+viewport!.width+1);
  expect(await board.evaluate(el=>getComputedStyle(el).scrollPaddingLeft)).toBe('220px');
});
test("phone pager defaults to Running and tiles and dots follow scrolling",async({page})=>{
  await page.setViewportSize({width:390,height:844});await page.goto('/#/jobs');
  const running=page.getByRole('button',{name:'Go to Running',exact:true});await expect(running).toHaveAttribute('aria-current','step');
  const assertAligned=async(key:string)=>{
    const geometry=await page.locator(`[data-station="${key}"] .station-heading`).evaluate(header=>{
      const board=header.closest('.pipeline-scroll')! as HTMLElement;
      const b=board.getBoundingClientRect(),h=header.getBoundingClientRect();
      return {left:Math.abs(h.left-b.left-board.clientLeft),right:h.right-(b.left+board.clientLeft+board.clientWidth),padding:getComputedStyle(board).scrollPaddingLeft};
    });
    expect(geometry.left).toBeLessThanOrEqual(1);
    expect(geometry.right).toBeLessThanOrEqual(1);
    expect(geometry.padding).toBe('0px');
  };
  await assertAligned('progress');
  await page.getByRole('button',{name:'Go to Needs you',exact:true}).click();
  await expect(page.getByRole('button',{name:'Go to Needs you',exact:true})).toHaveAttribute('aria-current','step');
  await assertAligned('attention');
  // A viewport resize (here the full-page capture) must keep the paged station.
  const settled=()=>expect.poll(async()=>{try{await assertAligned('attention');return true;}catch{return false;}},{timeout:3000}).toBe(true);
  await page.screenshot({fullPage:true});
  await settled();
  await page.setViewportSize({width:390,height:700});
  await settled();
  await page.waitForTimeout(600); // let the post-resize settle window pass before scrolling by hand
  const board=page.locator('.pipeline-scroll');await board.evaluate(el=>{el.scrollLeft=0;});
  await expect(page.getByRole('button',{name:'Go to Running',exact:true})).toHaveAttribute('aria-current','step');
  await expect(page.locator('.pager-dot[aria-current="step"]')).toHaveAttribute('aria-label','Running page');
  expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1);
});
test("theme defaults dark and header selection survives reload",async({page})=>{
  await page.emulateMedia({colorScheme:'light'});await page.goto('/#/jobs');
  await expect(page.locator('html')).toHaveAttribute('data-theme','dark');
  await page.getByRole('button',{name:'Switch to light theme',exact:true}).click();await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme','light');
});

test("Shipped expands in place and grouping and filters keep their data",async({page})=>{
 await page.goto('/#/jobs');
 const shipped=page.getByRole('region',{name:'Shipped',exact:true});
 const more=shipped.getByRole('button',{name:/^\+\d+ more$/}).first();
 const before=await shipped.getByRole('link').count();await more.click();
 expect(await shipped.getByRole('link').count()).toBeGreaterThan(before);
 await page.getByRole('group',{name:'Group production lines by'}).getByRole('button',{name:'Harness',exact:true}).click();
 await expect(page.locator('.line-rail h3').filter({hasText:'codex'})).toBeVisible();
 await page.getByRole('searchbox').fill('Share job summaries');
 await expect(page.locator('.pipeline-scroll article')).toHaveCount(1);
 if(panel)await expect(page.getByRole('button',{name:/^(Apply|Discard|Cancel|Resume)$/})).toHaveCount(0);
});

for (const width of [1440,390]) test(`filter disclosure keeps filtering at ${width}px`,async({page})=>{
 await page.setViewportSize({width,height:900});await page.goto('/#/jobs');
 const stepper=page.locator('.station-stepper');
 if(width===1440){
   await expect(stepper).toBeHidden();
   const centers=await page.locator('.lines-toolbar > .segmented, .lines-toolbar > .line-group-control, .lines-toolbar > .jobs-search, .lines-toolbar > .jobs-keyboard, .lines-toolbar > .filters-toggle').evaluateAll(nodes=>nodes.map(node=>{const box=node.getBoundingClientRect();return box.top+box.height/2;}));
   expect(Math.max(...centers)-Math.min(...centers)).toBeLessThanOrEqual(1);
 }else await expect(stepper).toBeVisible();
 const filters=page.getByRole('button',{name:'Filters',exact:true});
 await expect(filters).toHaveAttribute('aria-expanded','false');
 await expect(page.getByRole('combobox',{name:'Harness',exact:true})).toBeHidden();
 await filters.click();
 await page.getByRole('combobox',{name:'Status',exact:true}).selectOption('review');
 await expect(page.locator('.pipeline-scroll article')).toHaveCount(1);
 await expect(page.locator('.pipeline-scroll article')).toContainText('Share job summaries');
 await filters.click();
 await page.locator('h1').click();
 await page.keyboard.press('/');await expect(page.getByRole('searchbox')).toBeFocused();
});
test('belts sit behind the first card and light theme reaches the page and scrim',async({page})=>{
 await page.goto('/#/jobs');
 const geometry=await page.locator('[data-station="progress"] .line-cell').filter({has:page.locator('article')}).first().evaluate(cell=>{
   const card=cell.querySelector('article')!,belt=cell.querySelector('.belt')!;
   const b=belt.getBoundingClientRect(),c=card.getBoundingClientRect(),row=cell.getBoundingClientRect();
   return {beltZ:Number(getComputedStyle(belt).zIndex),cardZ:Number(getComputedStyle(card).zIndex),entry:b.top+b.height/2-row.top,insideCard:b.top+b.height/2>c.top && b.top+b.height/2<c.bottom,boundary:row.bottom-b.bottom,separator:getComputedStyle(cell).borderBottomStyle};
 });
 expect(geometry.cardZ).toBeGreaterThan(geometry.beltZ);
 expect(Math.abs(geometry.entry-46)).toBeLessThanOrEqual(1);
 expect(geometry.insideCard).toBe(true);
 expect(geometry.boundary).toBeGreaterThan(12);
 expect(geometry.separator).toBe('solid');
 await page.emulateMedia({reducedMotion:'reduce'});
 expect(await page.locator('.belt line').evaluateAll(lines=>lines.every(line=>getComputedStyle(line).animationName==='none'))).toBe(true);
 const body=()=>page.locator('body').evaluate(node=>getComputedStyle(node).backgroundColor);
 const dark=await body();await page.getByRole('button',{name:'Switch to light theme',exact:true}).click();
 expect(await body()).not.toBe(dark);expect(await body()).toBe('rgb(246, 245, 241)');
 expect(await page.locator('.v2-header, .pipeline-scroll, .pipeline .task-card').evaluateAll(nodes=>nodes.every(node=>getComputedStyle(node).backgroundColor==='rgb(255, 255, 255)'))).toBe(true);
 await page.getByRole('region',{name:'Running',exact:true}).getByRole('link').first().click();
 await expect(page.getByRole('dialog',{name:'Job details'})).toBeVisible();
 expect(await page.locator('.jobs-scrim').evaluate(node=>getComputedStyle(node).backgroundColor)).toBe('rgba(20, 20, 19, 0.35)');
});
});
