# Task cockpit-panel (D2-4) — dashboard Cockpit 滑出面板(前端)

## 目标(orchestrator-cockpit-spec E2:前端)
在 dashboard 加 **Cockpit 滑出面板**:与 orchestrator 座席的深度集成会话对话——会话事件流
(SSE)+ 输入框发 prompt + 审批卡(pending approval → allow/deny)+ cancel。走 D2-2 已上线的
serve 端点。**本地模式**特性(canOrchestrate),hosted 不启用。

## 已就位的后端(D2-2,serve 端点)
- `POST /api/projects/{id}/cockpit/prompt` {seat,text} → {ok,threadId}
- `GET  /api/projects/{id}/cockpit/stream?seat=` → SSE,每条 `data: <Event json>`(Event 形如
  `{kind:"message"|"tool"|"usage"|"state"|"error", text?, final?, tool?:{phase,name,text}, usage?, state?, err?}`)
- `POST /api/projects/{id}/cockpit/permission` {seat,approvalId,decision:"allow"|"deny"} → {ok}
- `POST /api/projects/{id}/cockpit/cancel` {seat} → {ok}
- `GET  /api/projects/{id}/cockpit/status?seat=` → {threadId, pending:[{id,kind,toolName}]}

## 改文件
- `web/src/lib/api.ts`(cockpit API 函数)
- `web/src/lib/datasource.tsx`(DataSource 加可选 cockpit 方法 + capability `cockpit`;LocalServeSource 实现)
- 新增 `web/src/components/CockpitPanel.tsx`
- 改 `web/src/App.tsx`(加开关按钮 + 挂载面板,gated 在 `capabilities.canOrchestrate`)
- 新增 `web/src/components/CockpitPanel.test.tsx`

## 契约
### api.ts
```ts
export async function cockpitPrompt(project: string, seat: string, text: string): Promise<{ ok: boolean; threadId: string }>
export async function cockpitRespond(project: string, seat: string, approvalId: string, decision: "allow"|"deny"): Promise<void>
export async function cockpitCancel(project: string, seat: string): Promise<void>
export async function cockpitStatus(project: string, seat: string): Promise<{ threadId: string; pending: {id:string;kind:string;toolName:string}[] }>
export function cockpitStreamUrl(project: string, seat: string): string  // 供 EventSource
```
用现有 writeJSON/fetch 模式(POST 走 writeJSON)。

### datasource.tsx
- `DataSourceCapabilities` 加 `cockpit: boolean`。LocalServeSource 设 `cockpit: true`(canOrchestrate 本地),relaysource 保持默认(不设即 falsy——若类型要求必填则 relaysource 设 false)。
- DataSource 接口加可选:`cockpitPrompt?`, `cockpitRespond?`, `cockpitCancel?`, `cockpitStatus?`, `cockpitStreamUrl?`。LocalServeSource 实现(转调 api.ts)。

### CockpitPanel.tsx
props: `{ project: string; seat: string; onClose: () => void }`。
- 挂载时:EventSource(src.cockpitStreamUrl(project,seat)),onmessage 累积事件到本地 state(把
  message.text 增量拼接成对话气泡;tool/state/usage/error 显示为系统行);卸载时 close()。
- 轮询或初始拉 `cockpitStatus` 得 pending approvals,渲染审批卡(显示 toolName + kind,两个按钮
  Allow/Deny → cockpitRespond → 刷新 status)。也可在收到 stream 事件后重拉 status。
- 底部输入框 + 发送:cockpitPrompt(project,seat,text)。
- Cancel 按钮:cockpitCancel。
- 样式**照 TaskDetail.tsx 的滑出 aside 骨架**(right slide-over,dark tokens var(--color-*),
  data-testid 命名);不要求像素级设计还原,功能 + 深色 token 一致即可。
- 防御:所有 src.cockpit* 方法可能 undefined(可选),用前判空。

### App.tsx
- 在合适位置(如 header 或 board 工具条)加一个按钮 `data-testid="cockpit-toggle"`,
  仅当 `src.capabilities.canOrchestrate && src.cockpitStreamUrl` 时渲染;点击 toggle 一个
  `cockpitOpen` state。open 时挂 `<CockpitPanel project={current} seat={<orchestrator 座席>} onClose=.../>`。
  seat 取当前项目 roster 里 orchestrator 角色的座席 id(state.agents 里 roles 含 "orchestrator";
  取第一个;取不到则用 author 座席名或 "claude")。

## 测试(CockpitPanel.test.tsx,vitest + @testing-library)
- mock 一个 DataSource(cockpit* 方法为 vi.fn;cockpitStreamUrl 返回一个 url)。用 fake EventSource
  (vi.stubGlobal("EventSource", class{...})或注入)推送几条 data 事件 → 面板渲染出对应气泡/系统行。
- 输入框输入 + 发送 → cockpitPrompt 被调用(project/seat/text 正确)。
- status 返回一个 pending → 审批卡出现 → 点 Allow → cockpitRespond 被调(approvalId,"allow")。
- onClose 按钮触发 onClose。
- App 层可加一条:canOrchestrate 时 cockpit-toggle 存在(可选,若简单)。

## 验收 / Acceptance(视角: correctness — SSE 累积渲染、审批往返、prompt 发送、capability 门、EventSource 卸载清理)
- reviewer 跑 tsc + vitest;并起 dev/build 确认无编译错。视觉最终门(playwright 实拍)由 reviewer 另跑。

## verify
verify: cd web && npx tsc --noEmit && npx vitest run src/components/CockpitPanel.test.tsx src/lib/datasource.test.tsx
