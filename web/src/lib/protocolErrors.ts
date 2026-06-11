// protocolErrors maps the engine's raw error strings (thrown verbatim through
// lib/api.ts from internal/pact/rules.go + internal/serve/author.go) onto
// actionable Chinese guidance. The output keeps the raw text in parens so the
// underlying protocol message stays debuggable: `${human}(${raw})`. Unknown
// errors pass through verbatim — we never hide a message we don't recognize.
//
// Each rule is a (substring, human) pair. The substrings are EXACT fragments
// the engine emits (no regex needed); first match wins, so order the more
// specific fragments before the broader ones where they could overlap.
const RULES: Array<{ match: string; human: string }> = [
  // Self-accept face: owner≠reviewer is enforced at assign, so an owner trying
  // to review/accept their own task always trips the reviewer-only gate.
  {
    match: "may review",
    human: "工蚁不能给自己的活盖章——只有 reviewer 坐席能验收,换个坐席操作",
  },
  // Separation of duties at assign time.
  {
    match: "must differ from reviewer (separation of duties)",
    human: "owner 和 reviewer 必须是不同坐席(职责分离)",
  },
  // Deps gate — fires at join AND checkpoint with the same fragment.
  {
    match: "blocked by unaccepted dep",
    human: "前置任务还没验收,门控会拦下这一步——先让依赖通过 accept",
  },
  // Acting seat not in the project roster (serve author layer).
  {
    match: "is not in the project roster",
    human: "当前坐席不在该项目的 roster,先用 pactify wire 接入这个坐席",
  },
  // Checkpoint by a non-owner.
  {
    match: "is not the owner of",
    human: "只有 owner 坐席能 checkpoint 这个任务",
  },
  // Review verb on a task that isn't awaiting_review.
  {
    match: "is not awaiting_review",
    human: "任务还没进入待评审状态,等 owner checkpoint 后再来验收",
  },
  // Dependency cycle (assign).
  {
    match: "introduces a dependency cycle",
    human: "依赖关系会形成环,检查一下任务之间的先后顺序",
  },
  // Same-feature dep constraint.
  {
    match: "must be in the same feature as",
    human: "依赖必须和任务在同一个 feature 内",
  },
  // Duplicate task id.
  {
    match: "already exists",
    human: "任务 id 已存在,换一个 id 或先删除旧任务",
  },
  // Merge gate — a feature task isn't accepted yet.
  {
    match: "not accepted",
    human: "feature 里还有任务没验收,全部 accept 后才能 merge",
  },
  // No acting seat configured at the serve layer.
  {
    match: "no acting seat configured",
    human: "没有配置操作坐席,启动时带上 --seat 或设置 PACT_AGENT_ID",
  },
];

export function humanizeError(raw: string): string {
  for (const r of RULES) {
    if (raw.includes(r.match)) return `${r.human}(${raw})`;
  }
  return raw;
}
