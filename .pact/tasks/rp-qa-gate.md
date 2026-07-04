# rp-qa-gate — QA-agent 门(实验性,任务级 opt-in)

> 母 spec:`docs/specs/review-runtime-deepening.md` §4。依赖 rp-fix-loop(FAIL 走 fix 轮)。

## 交付
1. 任务 spec 解析:`qa: <hint>` 行(与 verify: 同解析风格,找现有 verify 行解析处推广);无 qa: = 现状。
2. loop.go:verify 绿后发 QA 棒:briefing = `.pact/skills/qa-guide.md`(有则,包1 knowledge 管道复用)+ 任务上下文 + 「真跑软件验证 <hint>;报告写 `.pact/orchestrate/qa-<task>.md`;末行 `QA_RESULT: PASS|FAIL: <一句>`」。
3. FAIL → 进 fix 轮(与 rp-fix-loop 共享轮次上限);PASS/无标记 → 继续(无标记记 note,宽松)。QA 报告路径注入 reviewer briefing。

## 测试
PASS/FAIL/无标记三分支;FAIL 进 fix 轮;无 qa: 零变化 golden。`go test ./internal/orchestrate/ -run QA` 绿。
