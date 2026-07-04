# relay-client：抽出框架无关的 relay 读客户端到共享包（P3 后端）

> 先读 `cloud/web/src/lib/relay.ts`（`MissionControlRelay` —— 现有的 relay 连接：auth/HTTP/socket/decrypt，已框架无关但绑在 Svelte 包里）+ `cloud/crypto`（deriveProjectKey/decryptEvent）+ `cloud/wire`（PactEvent 等）。参考 `cloud/pact-project` 的包脚手架。

## 目标
把 relay **读客户端**抽成一个独立、框架无关、可测试的 cloud workspace 包 `@pactify-apps/relay-client`，供 Svelte(`cloud/web`) 与 React(`web/`) 共用。**不含任何 UI 框架依赖。**

1. **新包脚手架**（照 `cloud/pact-project`/`cloud/crypto`）：`cloud/relay-client/package.json`（name `@pactify-apps/relay-client`、version `0.1.0`、`publishConfig.access=public`、deps: `socket.io-client` + `@pactify-apps/crypto` + `@pactify-apps/wire` workspace:*、build=tsc、test=vitest）+ `tsconfig.json`（extends `../tsconfig.base.json`）。
2. **客户端**：`src/index.ts` 导出 `RelayClient` 类（从 `MissionControlRelay` 提炼），方法：
   - `login()`（挑战应答鉴权，拿 token）
   - `listProjects(): Promise<Project[]>`（GET `/v1/pact/projects`）
   - `getProjectEvents(projectId, afterSeq?): Promise<PactEvent[]>`（GET `/v1/pact/projects/:id/events`）
   - `subscribe(projectId, onEvent): () => void`（socket `pact-event`，返回取消函数）
   - `decrypt(projectId, bodyEnc): unknown`（`deriveProjectKey` + `decryptEvent`）
   - 构造函数接收 `{ url, master }`（master secret）——保持与 MissionControlRelay 相同的注入方式。
   - 类型（Project/PactEvent header）从 wire 或本包定义，勿重复造。
3. **测试**：`test/*.test.ts` 用 mock fetch + mock socket 覆盖 login/listProjects/getProjectEvents/decrypt/subscribe（不连真 relay）。decrypt 可用 crypto 的黄金向量往返验证。
4. **回接 Svelte**：`cloud/web/src/lib/relay.ts` 改为从 `@pactify-apps/relay-client` 复用（`MissionControlRelay` 要么继承/包装 `RelayClient`，要么直接换用），**保持 cloud/web 仍能 build + 其 board.test 绿**（别破坏正在服役的 Svelte 站）。

## 文件
- 建 `cloud/relay-client/`（package.json、tsconfig.json、vitest.config.ts、src/index.ts、test/）
- 改 `cloud/web/src/lib/relay.ts`（复用新包）+ 更新 `cloud/pnpm-lock.yaml`（pnpm install）
- 不碰 `cloud/relay`（服务端）、不碰 fly/Docker（部署面）

## 纪律
- **框架无关**：新包不 import svelte/react/fastify。纯 fetch + socket.io-client + crypto。
- **不破坏部署**：只加 workspace 包 + 改 cloud/web 消费方；`cloud/relay` 与 Docker/fly 一律不动。
- decrypt 语义与 MissionControlRelay 逐字节一致（同 deriveProjectKey + decryptEvent）。
- 完成跑 verify 绿再 checkpoint。

verify: cd cloud && pnpm install && pnpm --filter @pactify-apps/relay-client build && pnpm --filter @pactify-apps/relay-client test && pnpm --filter @pactify-apps/web build
