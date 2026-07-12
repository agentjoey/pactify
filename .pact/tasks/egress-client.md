# Task egress-client (egress 包 1/2) — RelaySource 增量投影 + after_seq 游标 + 轮询卫生

## 背景(已实锤的读放大)
staging Neon 11 天传出 5.17 GB,而热数据 <1 MB。主犯 relaysource.ts:491:每收一条 socket live
事件 → `await this.getState(id)` → `getProjectEvents(id)` **无游标整库拉取**重投影;另外
getState/fetchEventsLog/getEvents 三方法各自整库拉。relay REST 端点已支持 `after_seq` 游标
(cloud/relay-client getProjectEvents 的 opts?检查签名,不支持就给 client 加透传——server
端 GET /v1/pact/projects/:id/events?after_seq= 已实现)。

## 交付
### 1. per-project 解密事件缓存(RelaySource 内存态,绝不落 localStorage——解密内容)
`Map<projectId, { events: PactEvent[]; lastSeq: number }>`:
- 首拉:无缓存 → 全量 getProjectEvents → 解密 → 存缓存(记录最大 seq,取自 relay 行的 seq 字段)。
- 增量:有缓存 → `getProjectEvents(id, { afterSeq: lastSeq })` 只拉缺口 → 追加。
- **live 事件零网络**:subscribe 回调里不再 getState 重拉——把解密好的事件直接 append 进缓存
  (按 event_id 去重;注意 socket 事件无 relay seq,seq 以下次增量拉取对账,append 时仅去重),
  然后 `project(cache.events)` 本地重投影回调 onState。
- getState / fetchEventsLog / getEvents 全部走同一缓存(getEvents 需要的 header 字段
  seq/eventType/ts 一并缓存或按需保留原始行)。
- 断线重连(client.subscribe 的重连回调如有;没有则在下次 getState 时):afterSeq 增量补缺口。
- 缓存失效:decrypt 失败/项目切换不清缓存(按 project keyed);locked 模式不缓存不拉。
### 2. relay-client 游标透传
cloud/relay-client getProjectEvents 若无 opts 参数,加 `{ afterSeq?: number }` → query
`?after_seq=`。tsc+其 vitest 绿。
### 3. 轮询卫生(web/src/App.tsx)
- `document.visibilitychange`:hidden 时暂停 4s orchestrate-status 循环、5s cockpit-status
  轮询、60s tick;visible 时立即执行一次再恢复定时。抽一个 `useVisiblePoll(fn, ms)` hook 统一。
- hosted(RelaySource)下跳过 4s orchestrate-status 循环里的 `getOrchestrateStatus` 直连
  /api 调用(相对路径在 Vercel 上 404,纯噪音)——capabilities.multiMachine 时该循环整段不跑
  (runningByProject 在 hosted 用 machines/在线状态或空对象,现状本来就 404 全 false)。
## 测试
缓存首拉/增量 afterSeq 参数断言/live append 去重 + 本地重投影(mock client 断言 **不再** 因
live 事件调 getProjectEvents)/三方法共享缓存/locked 不拉;useVisiblePoll hook(fake timers +
visibilitychange 事件);hosted 跳过 status 循环。全量 vitest+tsc+build 绿。
verify: cd web && npx tsc --noEmit && npx vitest run && cd ../cloud/relay-client && npx tsc --noEmit && npx vitest run
