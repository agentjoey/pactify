# Task approval-risk-ui (E1b-a) — 审批卡危险分级

## 目标(cockpit spec §4.1/E1b:危险动作强审批)
审批卡现在有 toolName/kind/RawInput 但无危险分级——exec 和 read 长得一样。加 risk 分级贯穿
serve→panel,exec/write 视觉强化 + exec 需二次确认。

## 改动
### serve(internal/serve/cockpit.go)
- `cockpitPendingItem` 加 `Risk string \`json:"risk"\``。映射处用 **cockpit 包已有的分级**:
  给 cockpit 包加一个导出包装 `func GradeRisk(tool, detail string) string`(转调既有 gradeRisk,
  audit_grade.go),serve 调 `cockpit.GradeRisk(p.ToolName, string(p.RawInput))`。
### web(CockpitPanel.tsx + api.ts)
- status pending 类型加 `risk?: string`。
- 审批卡:risk 徽标(exec→danger 红、write→amber、mcp→blue、read→subtle 灰;小号 uppercase)。
- **exec 强审批**:risk=="exec" 时 Allow 按钮首次点击变成确认态("Confirm allow ▸",danger 底色,
  3s 内再点才真 Respond;超时或点别处復原)——照 Board 的 Override gate & merge 二段确认既有模式
  (RunRail.tsx:624-626 附近)。write/mcp/read 保持一键。
- Deny 永远一键。

## 测试
- serve:pending item 带 risk(EmitApproval 一个 bash 命令 → risk=="exec")。
- web:exec 卡显示红徽标 + 首点 Allow 不调 cockpitRespond、出现确认态、再点才调;read 卡一键即调。

## 验收(视角: security — 分级贯穿、exec 双确认、read 不加摩擦)
verify: cd web && npx tsc --noEmit && npx vitest run src/components/CockpitPanel.test.tsx && cd .. && go test ./internal/serve/ ./internal/cockpit/
