import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
const luminance = (hex: string) => hex.match(/\w\w/g)!.map(v => parseInt(v,16)/255).map(v => v <= .04045 ? v/12.92 : ((v+.055)/1.055)**2.4).reduce((sum,v,i) => sum+v*[.2126,.7152,.0722][i],0);
const ratio = (a:string,b:string) => { const values=[luminance(a),luminance(b)].sort((x,y)=>y-x); return (values[0]+.05)/(values[1]+.05); };
it("keeps faint metadata above AA on every panel surface in both themes", () => {
  const css=readFileSync("src/tokens.css","utf8");
  const blocks=[css.match(/:root\s*\{([\s\S]*?)\}/)![1],css.match(/:root\[data-theme="light"\]\s*\{([\s\S]*?)\}/)![1]];
  for(const block of blocks){
    const values=Object.fromEntries([...block.matchAll(/(--[\w-]+):\s*(#[\da-f]{6})/g)].map(m=>[m[1],m[2]]));
    for(const key of ["--bg","--bg-panel","--bg-elev","--bg-elev2","--bg-input"])
      expect(ratio(values["--text-3"],values[key])).toBeGreaterThanOrEqual(4.5);
  }
});
it("adapts the chart product colour and native Live bars to theme tokens", () => {
  expect(readFileSync("src/tokens.css","utf8")).toMatch(/:root\[data-theme="light"\][\s\S]*--color-role-product:\s*#/);
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
