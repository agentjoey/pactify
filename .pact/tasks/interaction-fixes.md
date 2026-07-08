# Task interaction-fixes (P2#18/#19) — ⌘K footer 条件化 + Review 卡内联 Changes 理由

## 1. ⌘K footer(根因已定位)
CommandK.tsx:323 的 "Write actions hidden in observe mode" 是**无条件静态文案**——author 模式也显示。
修:该 hint 仅当 `!showActions`(观察/回放态)显示;author 态显示 `esc Close`(或不显示第三项)。

## 2. Review 列 Changes 内联理由(#19 不对称)
现状:Review 列卡片 Accept 一键即走,Changes(Board.tsx:187)绕道打开侧栏找 textarea。
修:点 "↺ Changes" 后卡片上**就地展开**一行迷你表单(textarea 单行自增 + Send/Cancel 两钮,
data-testid="inline-changes-form"/"inline-changes-send"),Send 调既有 changes verb
(带 reason body,复用 Board/RightRail 现有 changes 调用方式,找到它并复用同一 src.verb 调用),
成功后收起+走现有刷新;Cancel 收起。空 reason 禁 Send。侧栏原路径保留不动。

## 改文件
web/src/components/CommandK.tsx · web/src/components/Board.tsx(或 TaskCard,看 Changes 按钮实际在哪)· 各自 test

## 测试
- CommandK:author=true 无 observe 文案;author=false 有。
- Board/TaskCard:点 Changes 出内联表单;空 reason Send 禁用;填 reason Send → verb 被调
  (project/task/reason 正确)且表单收起;Cancel 收起不调。

## 验收 / Acceptance(视角: ux — 文案不误导、Changes 与 Accept 对称、原路径零回归)
verify: cd web && npx tsc --noEmit && npx vitest run src/components/CommandK.test.tsx src/components/Board.test.tsx src/components/Board.verb.test.tsx
