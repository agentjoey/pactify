# relay-source：web/ 的 RelaySource（DataSource 的 relay 实现，只读）（P3 前端）

> 依赖 relay-client（`@pactify-apps/relay-client` 已就绪）。
> 先读 `web/src/lib/datasource.tsx`（`DataSource` 接口 + `LocalServeSource`，P1 已建）、`web/vite.config.ts` + `web/tsconfig.app.json`（cloud 包 alias 机制，已链好 pact-project/crypto/wire）、`@pactify-apps/pact-project`（`project(events)→State`）、`@pactify-apps/relay-client`。

## 目标
在 `web/` 实现 `RelaySource`（`implements DataSource`），走 relay 拉加密事件→解密→投影→State，作为托管 web 的数据源。**只读**：`capabilities.canWrite=false`（写/控制留到 U3 反向控制面）。

1. **链上 relay-client**：`web/vite.config.ts` resolve.alias + `web/tsconfig.app.json` paths 各加一行 `@pactify-apps/relay-client → ../cloud/relay-client/src/index.ts`（照现有 pact-project/crypto/wire 的写法，两处保持同步）。
2. **RelaySource**（`web/src/lib/relaysource.ts`，`implements DataSource`）：
   - 构造：`new RelaySource(client: RelayClient)`（注入，便于测试）。
   - `capabilities = { canWrite:false, canOrchestrate:false, multiMachine:true }`。
   - `listProjects()` → `client.listProjects()` 映射成 `ProjectMeta`。
   - `getState(id)` → `client.getProjectEvents(id)` → 每条 `client.decrypt(id, bodyEnc)` 还原完整 pact 事件 → `project(events)`（来自 `@pactify-apps/pact-project`）→ `State`。
   - `subscribe(id, onState)` → `client.subscribe(id, ...)`；到新事件则增量重取/重投影后 `onState(state)`；返回取消函数。
   - 写方法（verb/runOrchestrate/…）**不实现**（可选方法留空 undefined）——组件已按 `canWrite` 门控（P1 ds-wire 已就位）。
3. **测试**：`web/src/lib/relaysource.test.ts` 注入一个 mock `RelayClient`（返回构造好的事件），断言 `getState` 产出正确 `State`（复用 pact-project 的语义）、`listProjects` 映射、`subscribe` 回调 + 取消。**不连真 relay。**

## 文件
- 改 `web/vite.config.ts` + `web/tsconfig.app.json`（加 relay-client alias）
- 建 `web/src/lib/relaysource.ts` + `web/src/lib/relaysource.test.ts`
- 不改组件、不改 `LocalServeSource`（本地源不动）

## 纪律
- **只读**：canWrite=false，写方法不实现。不接 U3 控制。
- 复用 `@pactify-apps/pact-project` 投影（勿在 web 重造 events→State）。
- State 形状与 `web/src/lib/types.ts::State` 一致（组件零改即可渲染）。
- 完成跑 verify 绿再 checkpoint。

verify: cd web && npx tsc -b && npx vitest run src/lib
