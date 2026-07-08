# Task cockpit-panel-polish (P0#3 + P1#9/11/12/13/14) — Cockpit 面板打磨

## 目标
六项打磨,全部围绕 CockpitPanel + serve status:

### 1. 修实心填充上的文字不可见(P0,根因已定位)
tokens.css 的 `-ink == 主色` 是**给暗底彩字用的**(设计如此,别改 tokens 语义);CockpitPanel 把它
误用在**实心填充**上:
- 用户气泡(CockpitPanel.tsx:225):`bg-role-dev + text-role-dev-ink` → 绿底绿字不可见。
- Send 按钮(:318):`bg-role-design + text-role-design-ink` → 蓝底蓝字(实测就是个空蓝丸)。
- Approve 按钮(:278):`text-[var(--color-success-ink)]` —— **该 token 不存在**。
修法:在 tokens.css 加一个 `--color-on-accent: #0b0f16;`(深底色,给实心彩底上的文字),
三处 text 改用它。别动 role-*-ink 定义与其它正确用法(TaskDetail/Setup/ElementsGallery 是彩字用法,正确)。

### 2. 自动滚底(P1#9)
messages/systemRows/pending 变化时把消息容器 scrollTop=scrollHeight(ref;照 AgentTerminal.tsx:27 模式)。
若用户手动上滚(离底 >80px)则不强制拉底(简单判定即可)。

### 3. 运行中指示(P1#14)
收到 EventTool phase=="start" 且尚未收到对应 "end" 时,在消息区尾部显示一行运行指示
(如 `⏺ <toolname> running…` 带 subtle pulse);收到 tool end / state turn_completed / error 时清除。
简化:维护一个 runningTool string state,start 设,end/turn_completed/error 清。

### 4. threadId 显示(P1#12)
header 里 seat 名旁边小字显示 threadId(截前 8 位,title 全量;为空不显示)。threadId 从
status 响应取(已有),SSE cockpit/session 事件到达后也更新。

### 5. 轮询退避 + 失败提示(P1#13)
现固定 setInterval 2000。改:事件驱动为主(收到 tool/state 事件后拉一次 status)+ 兜底轮询 5s;
连续 3 次 status 失败 → 面板顶部细条 "Status unavailable — retrying…"(恢复后消失)。

### 6. 能力预检 + 友好错误(P1#11)
- serve:`handleCockpitStatus` 响应加 `capable bool` + `reason string`:session 已存在 → capable=true;
  否则按 seatKind 判定——kind ∈ {claude-code, codex-cli, kimi-cli, gemini-cli} → capable=true,
  空/其它 → capable=false + reason `seat "<seat>" has no deep-integration or ACP kind (kind=%q)`。
  (backendForKey 支持这四种,判定保持与其一致——可抽一个共享的 `cockpitCapableKind(kind) bool`。)
- 前端:status.capable==false 时输入框禁用 + placeholder 换 "This seat can't host a cockpit",
  面板顶部渲染 reason 的友好文案(非红色报错,是提示态);Prompt 网络错误的红字保留但**剥掉 URL 前缀**
  (只显示 error 字段内容)。

## 改文件
- `web/src/tokens.css`(+1 token)
- `web/src/components/CockpitPanel.tsx`
- `web/src/components/CockpitPanel.test.tsx`
- `internal/serve/cockpit.go`(status capable/reason)+ `cockpit_test.go`
- `web/src/lib/api.ts`(status 类型加 capable/reason)

## 测试
- serve:status 对无 kind 座席回 capable=false + reason;对已存在 session 回 true。
- web:capable=false → 输入禁用 + 提示渲染;tool start → running 指示出现,turn_completed 后消失;
  threadId 显示;自动滚底可用 scrollHeight mock 或略(至少不炸)。
- 原有 CockpitPanel 测试全部保持绿(mock 的 status 返回需补 capable:true)。

## 验收 / Acceptance(视角: ux — 文字可见、滚底、运行态、能力预检文案友好;token 语义不破)
verify: cd web && npx tsc --noEmit && npx vitest run && cd .. && go test ./internal/serve/
