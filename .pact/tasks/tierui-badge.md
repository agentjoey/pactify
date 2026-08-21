# tierui-badge — Badge 组件支持静音 token 与可访问名（顺带修既有 bug）

tier: L1
verify: cd web && npx vitest run src/components/ui/ && npx tsc -b --noEmit
dimension: correctness

## 目标 / Goal

让 `ui/Badge` 能承载 tier 徽标所需的三件事：静音语义色、`title`、以及**合法的**可访问名。
**本任务只改 Badge 及其测试，不碰任何业务页面。**

设计事实源：`.agent/frontend-design/exec-tiering-ui.md`（rev 3，已过两轮独立设计复核）。

## 已核实的事实（不要重新调查，直接用）

1. `Badge`（`web/src/components/ui/Badge.tsx:19-31`）只接受
   `color | colorVar | className | children`，**不透传 rest props**。
2. `BadgeColor` 联合（`Badge.tsx:6-17`）里**没有任何静音成员**——全是饱和的
   role/语义色。`--color-text-2` 不在联合内。
3. `colorVar` 的文档（`Badge.tsx:26-27`）写明是给 **runtime-computed** 颜色的逃生舱，
   *"the token union stays the encouraged path"*。tier 是**静态** token，
   走逃生舱会违反组件自身契约 → 应扩联合，不是滥用 `colorVar`。
4. ⚠️ **既有 bug**：`web/src/components/AccountPanel.tsx:87` 给 `<Badge>` 传了
   `data-testid="account-tier"`，因不透传而被**静默丢弃**——任何依赖它的测试都是
   "因错误原因而通过"。本任务顺带修掉。
5. ⚠️ **ARIA**：裸 `<span>` 上的 `aria-label` 在 ARIA 1.2 属 *name-prohibited*，
   支持不一致、可能被静默忽略。需要可访问名时必须带 `role`（如 `role="img"`），
   或用视觉隐藏文本。

## 改文件 / Files

- `web/src/components/ui/Badge.tsx`
- `web/src/components/ui/Badge.test.tsx`（新建或扩展）
- `web/src/components/AccountPanel.tsx`（**仅**为让既有 `data-testid` 生效，不做其它改动）

**不要**改 DispatchPanel / PlanDock / RunRail / 任何其它页面（那是下一棒）。

## 契约 / Contract

1. **`BadgeColor` 加入 `"text-2"`** —— 静音信息色。实测对比度（本项目四种背景下
   5.34–5.77:1）达标，无需再验。
2. **加受控 `title?: string`** —— 透传到根元素。不要改成 `...rest` 全透传
   （会削弱类型约束）；只加明确需要的 prop。
3. **可访问名**：加 `ariaLabel?: string`；**当且仅当**它非空时，根元素同时带
   `role="img"` 与 `aria-label`。不传时行为逐字节不变（不得给所有 badge 平白加 role）。
4. **`data-testid?: string`** 透传，修复第 4 条既有 bug；`AccountPanel.tsx:87`
   现有调用不改写法即应生效。
5. 既有 API（`color` / `colorVar` / `className` / `children`）与默认渲染
   **逐字节不变**——现有 21 处调用不得受影响。

## 验收 / Acceptance

评审维度：**correctness**。

- `color="text-2"` 渲染出 `var(--color-text-2)` 前景 + 15% alpha 底（与既有机制一致）。
- 传 `title` → 根元素有该 title；不传 → 无 title 属性。
- 传 `ariaLabel` → 根元素同时有 `role="img"` 与 `aria-label`；
  **不传 → 既没有 role 也没有 aria-label**（钉死，防止平白加 role）。
- 传 `data-testid` → 能被 `getByTestId` 取到；
  **回归**：`AccountPanel` 的 `account-tier` 现在真的可取到（此前静默丢弃）。
- 既有用法不回归：随便取两处现存调用（如 `Agents.tsx:70` 的 `color="warn"`）
  渲染结果不变。
- 门绿：`cd web && npx vitest run src/components/ui/ && npx tsc -b --noEmit`
