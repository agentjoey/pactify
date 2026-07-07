# Task relay-event-id (Track B) — relay 幂等键改 (projectId, event_id)

## 目标(spec: relay-ledger-idempotency-spec.md)
relay 现在按 `(projectId, seq)` 去重,seq=机器发的账本行号——账本被 git merge / union-merge /
snapshot 重排行号后,同一 `event_id` 落新行号 → relay 当新事件存 → 每次 serve 重启全量重放就累积
副本(staging 实测 2424 事件/51 份 init)。改用 `event_id`(随机 hash,明文放安全,零知识不破)。
决策 6:**不考虑 linx**,正常开发,仍走加性迁移(对 linx 天然无害)。

## 改文件
- `cloud/relay/prisma/schema.prisma`(PactEvent 加 nullable eventId + 加性 unique)
- `cloud/relay/src/pact.ts`(zod 加可选 eventId + ingest 带上)
- `internal/serve/relay.go`(pactIngestBody + buildBody 带 eventId)
- `docs/specs/agentworks-wire.md`(ingest 契约补 eventId 可选)
- 迁移文件:见 §迁移(可加 SQL,但 **DB apply 是人工部署步,不在本任务跑**)

## 契约
### prisma(schema.prisma 的 model PactEvent)
- 加字段 `eventId String?`(**nullable**,放在 seq 附近)。
- 加 `@@unique([projectId, eventId])`;**保留** `@@unique([projectId, seq])`。
- Postgres NULL 在 unique 里互不相等 → 旧行(eventId=null)+linx 行走老 `(projectId,seq)` 路,
  新 pactify 事件带 eventId 走新路。**纯加性。**

### pact.ts
- `PactIngestRequest`(.strict())加 `eventId: z.string().max(128).optional()`。
- `PactEventInput` interface 加 `eventId?: string`。
- `ingestPactEvent` 的 `db.pactEvent.createMany` 的 data 里带上 `eventId: input.eventId`(存在时;
  为 undefined 则存 null)。`skipDuplicates` 对**两个** unique 都生效:带 eventId 的重放命中
  `(projectId, eventId)` → no-op;不带的走 `(projectId, seq)`。`created: count>0` 语义不变。
- 若 ingest 的 HTTP handler 把 zod 解析结果映射到 PactEventInput,记得把 eventId 透传。

### 机器 internal/serve/relay.go
- `pactIngestBody` 加 `EventID string \`json:"eventId,omitempty"\``。
- `buildBody` 里 `EventID: ev.EventID`(event.Event 已有 EventID 字段,取自账本行 event_id)。**保留 Seq**。

### wire 契约 docs/specs/agentworks-wire.md
- ingest 请求契约补一行:`eventId`(可选,string,≤128)——稳定事件身份,明文安全(随机 hash)。

## 迁移(SQL 可提供,DB apply 是人工)
- 可在 `cloud/relay/prisma/migrations/` 加一个迁移(若你能不连 DB 生成 SQL);否则在 spec 注明
  部署时跑 `prisma migrate deploy`。迁移内容 = ALTER TABLE 加 nullable eventId 列 + CREATE UNIQUE INDEX
  on (projectId, eventId)。**加性、零停机。**（本任务不连 DB、不跑 migrate。）

## 部署顺序(人工,记档不在本任务执行)
relay 先部署(接受可选 eventId,向后兼容)→ 再更新机器二进制(开始发 eventId)→ staging DB 迁移
→ 一次性 resync。**因 zod .strict(),机器发 eventId 前 relay 必须先接受它。**

## 验收 / Acceptance(视角: security — 加性不破 linx/旧行、幂等键正确、zod 仍 strict 但接受 eventId)
- reviewer 跑 relay typecheck + go test ./internal/serve/ + 阅读确认加性、双 unique 保留。

## verify
verify: cd cloud/relay && npm run typecheck && cd ../.. && go test ./internal/serve/
