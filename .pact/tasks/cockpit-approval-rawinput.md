# Task cockpit-approval-rawinput (P0 安全) — 审批卡透出并渲染完整 RawInput

## 背景(review 发现 #1,违反 cockpit spec §4)
cockpit spec §4 定死:**审批卡信任根 = rawInput 全量渲染**(Title 是 agent 自由文本,可被
prompt-injection 伪装,不可作信任根)。现状:后端 `cockpit.PendingApproval.RawInput` 有,
但 serve 的 `cockpitPendingItem` DTO(internal/serve/cockpit.go:344)只透出 id/kind/toolName
——前端审批卡"盲批"。

## 改文件
- `internal/serve/cockpit.go`(DTO 加 rawInput + 映射)
- `web/src/lib/api.ts` + `web/src/lib/datasource.tsx`(cockpitStatus 返回类型加 rawInput)
- `web/src/components/CockpitPanel.tsx`(审批卡渲染参数)
- 各自测试

## 契约
### serve
- `cockpitPendingItem` 加 `RawInput json.RawMessage \`json:"rawInput,omitempty"\``;
  handleCockpitStatus 映射时带上 `p.RawInput`。
### web
- api.ts `cockpitStatus` 返回类型 pending 项加 `rawInput?: unknown`;datasource 同步。
- CockpitPanel 审批卡:在 toolName/kind 下方渲染一个**等宽小字号代码块**,内容
  `JSON.stringify(rawInput, null, 1)`(超长截断到 ~600 字符 + "…"),样式贴现有深色 token
  (bg 深一档、`overflow-auto max-h-32`)。rawInput 缺省时不渲染块(向后兼容)。
- **信任根语义**:参数块必须来自 rawInput 原文,不做任何"美化改写"(截断除外,截断要有省略号标记)。

## 测试
- serve cockpit_test:status 响应里 pending[0].rawInput 与 EmitApproval 注入的 RawInput 一致。
- CockpitPanel.test:status mock 带 rawInput → 审批卡出现代码块含参数内容;不带 rawInput 不炸。

## 验收 / Acceptance(视角: security — rawInput 原文透出、渲染不改写、缺省兼容)
verify: cd web && npx tsc --noEmit && npx vitest run src/components/CockpitPanel.test.tsx && cd .. && go test ./internal/serve/ ./internal/cockpit/
