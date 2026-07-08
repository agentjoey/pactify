# Task settings-autosave (review P3#26) — Agent configs 自动保存

## 问题
Settings → Agent configs 每张 agent 卡各带一个 Save 按钮(AgentConfig.tsx:~270),改 model/posture/
tools 后忘点 Save 即丢——多卡多按钮也啰嗦。

## 修法(AgentConfig.tsx)
- **去 Save 按钮,改自动保存**:model/restricted/tools 任一变化后 **debounce 600ms** 调既有 save 逻辑;
  保存中卡片右上角小字 "Saving…",成功后 "Saved ✓"(2s 淡出),失败红字 err(已有 err state)。
- 防误触:**初始加载(cfg 首次填充 state)不得触发保存**(用一个 hydrated ref/flag:首次 cfg→state
  同步完成后才启用 autosave effect)。
- tools 输入(逗号自由文本)在 debounce 下自然合并;blur 时立即 flush 一次(可选,若简单)。
- 保留 `scoped-${kind}` 等 data-testid(现有测试靠它们);**更新现有测试**:原"点 Save"用例改为
  "改值 → 等 autosave 调用 postAgentConfig"(vi.useFakeTimers 或 waitFor)。
- 状态字样 data-testid="autosave-state"。

## 验收(视角: ux — 改即存、初载不误存、失败可见、测试同步)
verify: cd web && npx tsc --noEmit && npx vitest run src/components/ops/AgentConfig.test.tsx
