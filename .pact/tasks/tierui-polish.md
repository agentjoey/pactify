# tierui-polish — 复核复验的 3 项残留（发布前收口）

tier: L1
verify: go test ./internal/serve/ && cd web && npx vitest run && npx tsc -b --noEmit
dimension: correctness

## 目标 / Goal

修掉独立复核复验（R4，结论 `approve-with-changes` / "releasable as-is"）留下的 3 项非门禁项。
Human Owner 已选择"先修再发"。**方向已定，不要重做设计，不要扩大范围。**

## 三项修复

### 1. `tier_raw` 的诊断只有鼠标能看到（与刚修的 F4 是同一缺陷）

`web/src/components/ui/TierBadge.tsx:61` 现在是 `ariaLabel={conflict || undefined}`。
但 `tier_raw` 的提示（`spec 写的是 "L9"，无法识别 —— 引擎将按 L1 运行`）在 `:57-59` 只进了
`title`，从未成为可访问名。实测该行 `role=None` / `aria-label=None` / `tabIndex=-1`
→ **只有鼠标悬停可达**——这正是上一轮修 `conflict` 时用的理由。

而且 `tier_raw` 比 conflict **更该被看到**：它指出的是人自己在 spec 里打错的字，改一行就能修。
同一组件里两条 title 诊断、一条有名一条没有，是上一轮改动引入的不一致。

**修**：把提示文案提取成一个 `const`，让 `title` 与 `ariaLabel` 共用：
`ariaLabel={conflict || tierRawMessage || undefined}`。
补测试——`ui.test.tsx` 里已有 `role="img"` 的断言可照抄。

### 2. 座席 kind 优先级逻辑重复

`internal/serve/orchestrate.go` 里 `seatKindsFromFold`（新增）与 `resolveSeatKinds`（既有，
run 路径仍在用）实现了**同一套**三级优先级：init 事件 kind → 名册 kind → 名称启发式。
新函数的注释自己也写着 "the same precedence as resolveSeatKinds"。
将来改优先级要改两处，而只有 run 路径有覆盖。

**修**：让 `resolveSeatKinds` **委托**——自己做读取，然后
`return seatKindsFromFold(evs, dto.Agents)`。一套实现，两个调用方。
不要反过来（不要让 `seatKindsFromFold` 去调 `resolveSeatKinds`，那会把第二次 fold 带回来）。

### 3. 把临时截图脚本正式化

`web/.shot-tier.tmp.mjs`（39 行）越出上一个任务的允许文件清单被提交，且以 `.tmp` 命名。
但复核读过两个脚手架后判定它**值得保留**：

- `web/scripts/shots.mjs` 打的是**长驻 daemon**（`SHOT_BASE` 默认 `127.0.0.1:17082`），
  而那个 daemon 可能跑着旧二进制里的陈旧 dist → **结构上无法保证"截图来自最终 build"**。
- 这个脚本 spawn `e2e/mock-server.mjs`，后者直接服务 `internal/serve/dist`（提交的产物）→ **可以保证**。

两者契约不同（"指向已运行的服务" vs "spawn 一个 hermetic mock"），**不要合并**。

**修**：改名为 `web/scripts/shot-dispatch-review.mjs`（按它拍什么命名，不按工单号），并修四处：

1. 硬编码的 `const WEB = "~/..."` —— 改为从 `import.meta.url` 推导
   （照抄 `e2e/mock-server.mjs` 的做法）
2. 硬编码输出 `/tmp/pactify-shots/dispatch-review-tier.png` —— 支持 `SHOT_OUT` 环境变量，
   与 `shots.mjs` 对齐
3. 硬编码端口 `4173` —— 那是 `playwright.config.ts` 的 `webServer` 端口，且
   `reuseExistingServer: !CI` 会让并发的 `playwright test` 撞车或静默连到**别的**服务器。
   改为从环境变量取，给一个不冲突的默认值。
4. 它在 `page.goto` **之后**才调 `/__test/reset`，而 `e2e/helpers.ts` 是**先 reset 再 goto`。
   照 helpers 的顺序改。

并在 `CLAUDE.md` 的「视觉门」那条后面**追加一句**说明该脚本用途（它目前全仓零引用＝隐形债）。
⚠️ 只追加一句，**不要重写 CLAUDE.md 其它内容**。

## 改文件 / Files

- `web/src/components/ui/TierBadge.tsx` + `web/src/components/ui/ui.test.tsx`
- `internal/serve/orchestrate.go`（+ 必要时测试）
- `web/.shot-tier.tmp.mjs` → `web/scripts/shot-dispatch-review.mjs`（git mv 后修改）
- `CLAUDE.md`（**只追加一句**）

**不要**改设计方向、不要动 `Badge.tsx`、不要重构无关代码。

## 验证与证据（顺序重要）

1. 门：`go test ./internal/serve/` + `cd web && npx vitest run && npx tsc -b --noEmit`
   + `npx playwright test`
2. **门全绿后**才出图：`cd web && npm run build`，然后用**改名后的**脚本出图
   （顺便验证它改完还能跑）
3. ⚠️ **验证"截图来自最终 build"不要用文件时间戳** —— 每次验证性重建都会把 dist 的 mtime
   推到截图之后，这个坑本轮已经踩过两次。正确做法：**dist 文件名本身就是内容哈希**，
   重建到一个 scratch 目录后比对文件名/字节是否一致即可。把比对结果写进 evidence。

## 验收 / Acceptance

评审维度：**correctness**。

- **1**：`tier_raw` 行的徽标同时有 `role="img"` 与 `aria-label`（内容含原始值提示）；
  既无 conflict 也无 tier_raw 的普通行**仍然**没有 role/aria-label（回归钉死）。
- **2**：`resolveSeatKinds` 委托给 `seatKindsFromFold`，优先级逻辑只有一份；
  run 路径行为不变（既有测试保持绿）。
- **3**：`web/.shot-tier.tmp.mjs` 不再存在；`web/scripts/shot-dispatch-review.mjs` 存在且
  四处硬编码/顺序问题均已修；脚本能实际跑出图；`CLAUDE.md` 视觉门处多了一句说明。
- 门全绿；截图来自最终 build，且**用内容哈希比对**（非 mtime）证明，结果写进 evidence。
