import { describe, it, expect } from "vitest";
import { humanizeError } from "./protocolErrors";

// The substrings below are lifted VERBATIM from the engine
// (internal/pact/rules.go, internal/serve/author.go) — these are the exact
// strings the author API throws through lib/api.ts. humanizeError prepends an
// actionable Chinese gloss and keeps the raw text in parens for debugging.
describe("humanizeError", () => {
  const cases: Array<[string, string]> = [
    // self-accept / wrong reviewer — owner accepting their own task hits the
    // reviewer-only gate (owner≠reviewer always), so this is the self-accept face.
    ["pactify accept: only the reviewer (rev) may review t1; you are alice", "工蚁不能给自己的活盖章"],
    // separation of duties at assign
    ["pactify assign: owner (alice) must differ from reviewer (separation of duties)", "owner 和 reviewer 必须是不同坐席"],
    // deps gate at join / checkpoint
    ["pactify join: task t2 blocked by unaccepted dep t1", "前置任务还没验收"],
    ["pactify checkpoint: task t2 blocked by unaccepted dep t1", "前置任务还没验收"],
    // seat not in roster (serve author.go)
    ['acting seat "ghost" is not in the project roster', "当前坐席不在该项目的 roster"],
    // checkpoint owner mismatch
    ["pactify checkpoint: alice is not the owner of t1 (owner: bob)", "只有 owner 坐席能 checkpoint"],
    // review on wrong status
    ["pactify accept: t1 is not awaiting_review (status: in_progress)", "任务还没进入待评审状态"],
    // dependency cycle
    ["pactify assign: task t1 introduces a dependency cycle", "依赖关系会形成环"],
    // same-feature dep
    ["pactify assign: dep t1 must be in the same feature as t2 (feat)", "依赖必须和任务在同一个 feature"],
    // duplicate task
    ["pactify assign: task t1 already exists", "任务 id 已存在"],
    // merge gate
    ["pactify merge: cannot merge feat; task t1 not accepted", "feature 里还有任务没验收"],
    // no acting seat
    ["no acting seat configured (set --seat or PACT_AGENT_ID)", "没有配置操作坐席"],
  ];

  for (const [raw, gloss] of cases) {
    it(`maps "${raw.slice(0, 40)}..."`, () => {
      const out = humanizeError(raw);
      expect(out).toContain(gloss);
      // raw is preserved verbatim in parens for debuggability
      expect(out).toContain(`(${raw})`);
    });
  }

  it("returns unknown errors verbatim (no parens wrapping)", () => {
    const raw = "some totally unexpected failure 502";
    expect(humanizeError(raw)).toBe(raw);
  });

  it("handles empty/whitespace input verbatim", () => {
    expect(humanizeError("")).toBe("");
    expect(humanizeError("   ")).toBe("   ");
  });

  it("format is `${human}(${raw})` for a known match", () => {
    const raw = "pactify assign: task t1 already exists";
    expect(humanizeError(raw)).toBe(`任务 id 已存在,换一个 id 或先删除旧任务(${raw})`);
  });
});
