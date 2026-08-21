# tierui-render — plan review 与运行时的 tier 呈现

tier: L2
verify: cd web && npx vitest run && npx tsc -b --noEmit
dimension: ux

## 目标 / Goal

在 plan review（DispatchPanel + PlanDock）显示 tier / verify / dimension·role，
在运行时（RunRail）显示 tier 与已解析 effort。

依赖：`tierui-backend`（DTO 字段）与 `tierui-badge`（组件能力）均已 accepted。
设计事实源：`.agent/frontend-design/exec-tiering-ui.md`（rev 3，**已过两轮独立设计复核**）。

## 设计约束（复核实测得出，不要自行改动）

1. ⚠️ **tier 徽标必须是行内第一个元素，占固定宽槽**（`w-[34px] shrink-0`），
   任务 id 用 `min-w-0 truncate` 放其后。
   **理由（关键）**：四档用**同一个**静音色、只靠文字区分；若徽标位置随 id 长度浮动，
   人就只能逐行读，而验收标准要的是"**扫读**"。固定 x 轴让 `L1 L1 L3 L1` 成为可扫的竖列——
   眼睛扫的是**位置+字形**，不靠色相。**不要**改成"徽标放行尾"或"跟在 owner 后面"。
2. ⚠️ **不要**用 `warn`/`danger` 表示 tier 档位高低。这两个 token 在本仓是**健康态**语义
   （`Agents.tsx:70` "Not detected"、`RunRail.tsx:645` "gate failed"）。
   一个 L3 任务是**分类正确且健康**的，标红会读成报警。
   四档一律 `color="text-2"`；`warn` **只**留给 NO TIER 这种真异常。
3. ⚠️ **dimension / role 不做徽标**：`MAINTAINABILITY` 单个徽标≈110px，占 312px 内容宽的 1/3；
   且 `role` **无后端校验**可为任意长串。降为一行静音文本（`correctness · frontend`）。
4. 面板固定 `w-[360px]` **无断点**，内容宽≈312px。**不要**写媒体查询或断点，
   也不要改面板宽度（<360px 溢出是既有问题，明确超出本次范围）。

## 改文件 / Files

- `web/src/lib/types.ts` —— `PlanTaskReview` 加字段
- `web/src/components/shell/DispatchPanel.tsx`
- `web/src/components/shell/PlanDock.tsx`
- `web/src/components/board/RunRail.tsx`
- 对应测试

**不要**改 `Badge.tsx`（上一棒已完成）、不要改后端、不要改面板宽度策略。

## 契约 / Contract

### A. DispatchPanel 任务行

```
[TIER]  task-id（truncate）
        owner → reviewer · deps: a,b
        verify: go test ./internal/foo/   ← truncate + title 全文
        correctness · frontend            ← 静音文本行,dimension·role,缺则整行不出
```

- tier 徽标：`color="text-2"`，文字为 `L0|L1|L2|L3`
- **`TierMissing==true`** → 文字 `NO TIER`，`color="warn"`，
  并传 `ariaLabel="未标注 tier —— 引擎将按 L1 运行"`（组件会带 `role="img"`）
- **`TierConflict` 非空** → 该徽标 `title` 用冲突文案
- 每个 tier 徽标都带 `title` 说明该档含义
- **列表上方一行可见图例**：`L0 便宜 · L1 默认 · L2 复杂 · L3 高风险`
  （`title` 是鼠标专属，键盘/触屏用户看不到，必须有可见图例）

### B. PlanDock

**只显示 tier 徽标**（同 A 的规则），**不显示** verify / dimension / role ——
它比 312px 更窄。其余保持原样。

### C. RunRail 运行时徽标

- 显示 tier；effort 非空时一并显示
- ⚠️ **`Effort === ""` 是多数情况**（仅 claude-code/codex-cli 支持 effort），
  应表达为"该 kind 未应用 tier 路由"之类的中性说明，**不要**渲染成空值或第二个量级
- ⚠️ **并发安全**：`Status` 是**单**快照，`--concurrency>1` 时会在并发任务间跳变 →
  徽标**必须**满足 `chip.id === status.task` 才渲染，否则会贴错 chip
- `status.task === ""`（feature 级升级）→ 不挂徽标
- 旧 status（无这些字段）→ 不挂徽标、不报错

## 验收 / Acceptance

评审维度：**ux**。

- 四档 + NO TIER 五种渲染各有测试；`TierMissing` 时文字为 `NO TIER` 且带 `role="img"` 与 aria-label。
- **固定位置**：tier 徽标是行内首元素且宽度固定——用一个超长 task id 与一个超短 id
  各渲染一次，断言徽标的布局类名不变（不随 id 长度改变）。
- 图例可见（不是 `title`）。
- dimension/role 缺失时该行不渲染（不出现孤零零的 `·`）。
- PlanDock 有 tier、**无** verify/dimension/role。
- RunRail：`chip.id !== status.task` 时**不**渲染徽标（并发贴错防回归）；
  `status.task===""` 不渲染；`effort===""` 走中性说明不报错。
- 门绿：`cd web && npx vitest run && npx tsc -b --noEmit`
- ⚠️ **仓库视觉门（CLAUDE.md 硬性）**：`npx playwright test` 绿 + `node web/scripts/shots.mjs`
  截图实测。**截图必须来自最终 build**。把命令输出与截图路径写进 checkpoint evidence。
