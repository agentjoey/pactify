# Task ui2-shell (UI v2 包 1/5) — token 对齐 + 共享 toolbar/三段镜头 + 视图路由收敛

## 设计源(必读)
- ~/AgentWorks/Code_Claude/design_handoff_pactify_ui/README.md(Design Tokens / Shared Toolbar /
  Keyframes / Interactions·Lens 节)
- 任一 designs/*.dc.html 的 toolbar 与 <svg viewBox="0 0 30 18"> wordmark(五屏共享)
**定案**:字体统一 Inter(README 允许的单字体路线),不引入 Space Grotesk;emerald=系统/成功、
gold=orchestrator/项目 的 accent 语义必须保留。

## 交付
### 1. tokens.css 对表补齐(现有深色基底已同源,只补缺/别推翻)
按 README Design Tokens 表核对:--bg-elev2(#171e2a)/--bg-input(#0f141d)/--bg-code(#07090d)/
--border(#222b3a)/--border-2/文本四级/角色状态色(gold #ffd479/blue #8ab4ff/emerald #6ee7a0/
ops #e0884a/err #f97066)。缺的加,已有等价 token 建立别名映射注释,禁止大规模改名(≈70% 组件
经 var() 引用,改名=全仓炸)。
### 2. keyframes 四件套(index.css 或 tokens.css)
breath/ring/eq/blink 按 README 参数;`prefers-reduced-motion: reduce` 下全部禁用。
### 3. 共享 Toolbar 重构(web/src/components/shell/Toolbar.tsx)
- 左:3-stroke SVG wordmark(从 .dc.html 抄 path,gold/blue/green)+ `pactify` 700;
  项目选择器保持现位(Board/Cockpit 语境在左簇,照现有 ProjectMenu)。
- 中:**三段镜头 Dashboard | Board | Cockpit**(segmented,border --border/bg --bg-input/
  radius 9px/3px pad;active 段 #171e2a 填充);**Cockpit 段内嵌待审批徽标**(err 色 breath
  红点 + mono 计数,来源 = cockpit pending 数,现有 status 轮询/订阅里取;0 时不显)。
  快捷键 1/2/3 切镜头。
- 右:⌘K(现有)· live pill(emerald,ring 脉冲点,`live` 文案——现有 live 状态接入)·
  Settings gear(切到 settings 视图,active 时 emerald)· 当前用户头像 tile(monogram 渐变,
  hosted 有邮箱显首两字母,本地显 cl)。
### 4. App 视图路由收敛(web/src/App.tsx)
- 视图状态:`lens: "dashboard" | "board" | "cockpit" | "settings"`(localStorage 持久,
  默认 board;dashboard 本包先渲染占位空视图,T4 做)。
- **Cockpit 从滑出面板升格为全页视图**:本包只做"挂法迁移"——lens=cockpit 时全页渲染现有
  CockpitPanel(宽度自适应,去滑出容器/关闭钮,内容原样;T3 重做内部)。cockpit-toggle 按钮
  与旧滑出逻辑删除(入口=镜头段)。
- **Settings 从 modal 升格为独立视图**:lens=settings 时全页渲染现有 SettingsModal 的内容区
  (去 modal 壳/遮罩/Escape 关闭改为切回上一镜头;T5 重做布局)。toolbar gear 与 ⌘K 的
  Settings 入口全部改到该视图。
- Board 内 Board|Flow 子切换与 RunRail/EventDrawer/RightRail 等 Board 域内容不动。
### 5. 测试与视觉
- Toolbar 三段镜头切换/徽标计数/gear 路由的组件测试;App lens 路由与持久化测试;既有测试
  相应迁移(cockpit slide-out 相关断言改全页)。全量 vitest + tsc + build 绿。
- 不追求像素完美(T2-T5 逐屏对齐),但 toolbar 必须照设计稿(实拍对比)。
verify: cd web && npx tsc --noEmit && npx vitest run
