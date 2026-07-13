---
id: phase0-h3
feature: phase0-sec
owner: kimi-worker
reviewer: claude
---

# phase0-h3 — 修复 H3：ingest 的 projectId 不校验归属 → 跨租户注入/顶替（high）

## 目标 / Goal
`POST /v1/pact/ingest` 直接用客户端 body 里的 `projectId`，**不校验调用方是否拥有该 project**；`PactEvent` 唯一性是全局 per-project。于是外部账号能：(a) 往受害者 project 注入伪造事件（被受害者看板折叠出来，因为读只按 projectId 过滤）；(b) 预占 `(projectId, seq)` 使受害者真事件被 `skipDuplicates` 丢弃。

## 改文件 / Files（只碰这些）
- `cloud/relay/src/server.ts`（`POST /v1/pact/ingest` handler）
- `cloud/relay/src/pact.ts`（`getProjectEvents` 纵深防御，可选）

## 契约 / Contract
1. **【必做·测试门控】** `server.ts` 的 ingest handler：在调用 `ingestPactEvent(db, accountId, parsed.data)` **之前**，查既存 project 归属——
   ```ts
   const existing = await getProject(db, parsed.data.projectId)
   if (existing && existing.accountId !== accountId) {
     return reply.code(404).send({ error: 'not found' })  // 与既有 foreign-read 一致，避免存在性探测
   }
   ```
   （`getProject` 已在 read handler 用到、已 import。project 不存在则放行——在调用方名下创建。）
2. **【纵深防御·可选】** `pact.ts` 的 `getProjectEvents` 的 where 加 `accountId` scope；调用处（read handler）传入 accountId。若改动 getProjectEvents 签名会波及其它调用者且超出范围，可只做第 1 步（第 1 步已让红测转绿）。
3. 不改其他行为；同账号 ingest/read 全部照旧。

## 验收 / Acceptance（dimension: security）
- `cd cloud/relay && npx vitest run test/pact.http.test.ts` —— H3 两条 cross-tenant 测试**转绿**（现红），且既有 pact HTTP 测试**不破**。
- `npx vitest run test/pact.test.ts` —— ingest 单元测试不破。
- `npx tsc -b --noEmit`（或 relay 的类型检查）干净。

verify: cd cloud/relay && npx vitest run test/pact.http.test.ts test/pact.test.ts
