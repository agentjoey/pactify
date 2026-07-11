# Task ui2-settings-setup (UI v2 包 5/5) — Settings 独立页布局 + Setup 向导刷新

## 设计源(必读)
README.md §4 Settings + §5 Setup + designs/"Pactify Settings.dc.html"、"Pactify Setup.dc.html"
整文件。字体统一 Inter。

## 交付
### A. Settings 独立页(T1 已挂全页,本棒重做布局;内容面板复用现有)
- 250px nav rail(--bg-panel):"Settings" 标题 + 搜索框(过滤 nav 项)+ scope 分组导航
  (组头=彩色方块+uppercase mono 标签:PROJECT·<项目> gold/MACHINE·this computer emerald/
  ACCOUNT blue);nav 项=线性 SVG 图标+标签,active=--accent-2 填充+--accent-line 边+emerald
  图标,hover=--bg-elev2。**hosted gating 保留**(hosted 只 ACCOUNT 组)。
- Pane:头(uppercase mono scope 行 + 22px 标题 + scope pill)+ 现有各面板内容
  (Seats/Wiring/ReviewGate/Worktrees/RegisteredAgents/AgentConfig/Sessions/Account/
  Machines/Appearance/Shortcuts)迁入新布局。
- Agent configs 卡对齐设计:36px monogram tile + name + cli id + effective-config 副行 +
  drivable/manual pill;manual 卡整体 60% 透明只读;Model 下拉 + Permission posture 段控
  (Blanket/Scoped;Scoped 展开 Allowed tools chips + 虚线 ＋add)+ Save(emerald)——
  现有 AgentConfig 能力对齐样式,无的能力(如 allowed tools 若后端无字段)做 UI 占位并注记。
### B. Setup 向导刷新(evolve web/src/components/Setup.tsx)
- Journey stepper(0 Setup gold active → 1 Plan → 2 Run → 3 Review → 4 Ship,2px rail 连接);
- 标题区("Wire your agents into the project" + PROJECT pill + ✓ Roles complete emerald pill);
- Seat rows:monogram + name + cli id + drivable pill + ROLES 标签 + **角色 toggle pills**
  (orchestrator/reviewer/worker,选中按角色色 tint + inset ring);
- 分权校验 callout(orchestrator/reviewer/worker 齐 + 无自审 → emerald 确认,否则提示)——
  现有校验逻辑对齐文案;
- Project location 卡(path mono 输入蓝 focus ring + blink 光标 + name 输入);
- Apply 主钮(蓝,`Apply · init + wire →`)+ OR COPY THE COMMANDS 代码块(--bg-code pre,
  现有 applyCommands 输出 + Copy 钮)。
### 5. 测试与视觉
Settings rail 导航/搜索过滤/hosted gating 回归;Setup 角色校验/commands 生成;全量绿;
playwright 实拍两屏对比设计稿。
verify: cd web && npx tsc --noEmit && npx vitest run
