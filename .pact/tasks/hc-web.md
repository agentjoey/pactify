# Task hc-web (E3-3/E4) — hosted CockpitPanel + Board 情境入口 + 座席选择(worker attach)

## 交付
### 1. relay-client(cloud/relay-client)
- acct 房间 `event` 监听(relay courier 转发的 ephemeral WireMessage:{runId,seq,body})暴露
  `onEphemeral(cb)`;按 runId 前缀 "cockpit:" 过滤归类。web 侧解密复用 decryptRaw(projectId, body)。
### 2. DataSource 抽象收口(web/src/lib/datasource.tsx)
- 加可选 `cockpitSubscribe?(project, seat, onEvent): () => void`:
  - LocalServeSource:内部用 EventSource(cockpitStreamUrl)实现(把 CockpitPanel 里的裸
    EventSource 逻辑搬进来,panel 不再自己 new EventSource)。
  - RelaySource:发 `cockpit.subscribe` rpc + 每 4min 重发续订;onEphemeral 解密→按 seq 排序
    去重→回调。返回的清理函数停止续订与监听。
- RelaySource 实现 cockpitPrompt/cockpitRespond/cockpitCancel/cockpitResume(对应 rpc,
  fire-and-forget,效果经订阅流回来);cockpitStatus 由订阅首条 snapshot(kind "status")喂
  (RelaySource 内部缓存最近 snapshot,cockpitStatus 返回它;无则 pending 空+capable true)。
- capabilities.cockpit 在 RelaySource 改 true。
### 3. CockpitPanel 适配(web/src/components/CockpitPanel.tsx)
- 改用 src.cockpitSubscribe(去裸 EventSource);hosted 下无 SSE 重连语义,清理函数即退订。
- 429(限速)错误 toast/inline 提示。
### 4. Board 情境入口
- Board 的 escalation/review 相关卡(RunRail/TaskDetail 里 escalated 或 awaiting_review 态)
  加「Discuss in Cockpit →」小按钮:切到 Cockpit 视图并预选该任务 owner 座席。
### 5. E4 座席选择(worker attach)
- CockpitPanel 加座席下拉:列出 roster 中 cockpit-capable kind 的座席(orchestrator 默认选中,
  worker 座席可选=attach)。切换座席=切换订阅与会话(Manager 本就 per (project,seat))。
## 测试
- datasource 两实现的 cockpitSubscribe 单测(Local:mock EventSource;Relay:mock client
  的 onEphemeral+sendRpc,断言订阅 rpc、续订、排序去重、清理);panel 座席切换、Discuss 入口
  跳转;全量 vitest 绿 + tsc + build。
verify: cd web && npx tsc --noEmit && npx vitest run
