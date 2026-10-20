import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
const luminance = (hex: string) => hex.match(/\w\w/g)!.map(v => parseInt(v,16)/255).map(v => v <= .04045 ? v/12.92 : ((v+.055)/1.055)**2.4).reduce((sum,v,i) => sum+v*[.2126,.7152,.0722][i],0);
const ratio = (a:string,b:string) => { const values=[luminance(a),luminance(b)].sort((x,y)=>y-x); return (values[0]+.05)/(values[1]+.05); };
it("keeps faint metadata above AA on every panel surface in both themes", () => {
  const css=readFileSync("panel/panel.css","utf8");
  const colors=css.match(/--color-text-3:\s*light-dark\((#[\da-f]+),\s*(#[\da-f]+)\)/)!;
  expect(colors).not.toBeNull();
  for (const [text, backgrounds] of [[colors[1],["#f5f7fa","#ffffff","#eef1f6","#f4f6f9"]],[colors[2],["#0a0e14","#10151e","#171e2a","#0c1119"]]] as const)
    for (const background of backgrounds) expect(ratio(text,background)).toBeGreaterThanOrEqual(4.5);
});
it("adapts the chart product colour and native Live bars to theme tokens", () => {
  expect(readFileSync("panel/panel.css","utf8")).toMatch(/--color-role-product:\s*light-dark\(/);
  const css=readFileSync("src/components/jobs/live.css","utf8");
  expect(css).toContain("::-webkit-meter-optimum-value");
  expect(css).toContain("::-moz-meter-bar");
  expect(css).toContain("::-webkit-progress-value");
});
it("allows Live content to scroll without a sticky footer clipping it", () => {
  const css=readFileSync("src/components/jobs/jobs.css","utf8");
  expect(css.match(/\.detail-footer\s*\{([^}]*)\}/)![1]).not.toContain("sticky");
});

it("reserves footer space independently from the modal content scroll area", () => {
  const css=readFileSync("src/components/jobs/jobs.css","utf8");
  expect(css.match(/\.jobs-drawer\s*\{([^}]*)\}/)![1]).toContain("overflow: hidden");
  const scroll=css.match(/\.detail-scroll\s*\{([^}]*)\}/);
  expect(scroll).not.toBeNull();
  expect(scroll![1]).toContain("overflow-y: auto");
  expect(scroll![1]).toContain("min-height: 0");
  expect(css.match(/\.detail-footer\s*\{([^}]*)\}/)![1]).toContain("flex-shrink: 0");
});
