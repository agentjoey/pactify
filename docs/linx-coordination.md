# linx 对接清单 — 多机编排（M2–M5）需要 linx 侧配合的共享改动

> 背景：pactify 与 linx 共用 relay（`@pactify-apps/relay`）+ wire（`@pactify-apps/wire`）。多机编排的绝大部分是 pactify 自己的 serve/驱动侧改动；本文件只列**触及共享 relay/wire、需要 linx 对应调整**的项。pactify 先上线（下面每项标了 pactify 侧已做什么），linx 按此调整即可互通。
>
> 原则：能做成 **additive/非破坏** 的都做成 additive（linx 只需 `pnpm up @pactify-apps/wire` 拉新版 + relay 重部署，零代码改动）；真正需要 linx 改代码的只有 auth 硬化两项。

---

## A. wire `RpcRequest` 新增 pactify rpc 类型（additive，linx 零代码改动）

pactify 在共享 `cloud/wire/src/rpc.ts` 的 `RpcRequest` discriminated union 里**追加**了这些类型（均带 `machineId`，relay 按现有机制路由，**relay 代码无需改**）：

| type | 用途（M 阶段） | 字段 |
|---|---|---|
| `pact.assign/accept/changes/merge/checkpoint` | U3（已上线） | machineId, project, + verb 参数 |
| `pact.stint` | M2 远程单棒（已上线） | machineId, project, task, seat, agentKind, briefing?, branch? |
| `orchestrate.run` / `orchestrate.resume` | M3 远程编排（已上线） | machineId, project, feature?, seatKinds? |
| `plan.generate` / `plan.apply` | M4 plan 托管（已上线） | machineId, project, goal?, feature, plannerKind? |
| `pact.provision` | M5 远程 clone 项目（已上线） | machineId, repoUrl, name |

**linx 侧要做的**：`pnpm up @pactify-apps/wire@<新版>` + relay 带新 wire 重部署。linxd 的 rpc handler 对 `pact.*`/`orchestrate.*`/`plan.*` 无 case → 自动忽略（和 pactify 忽略 linx 的 spawn/send-message 对称）。**已由 pactify hermetic 测试验证 relay 路由这些类型正常。**

---

## B. Auth 硬化（**需要 linx 改 relay 代码**，M2 前置）

这两项是**真正需要 linx 动 relay 的**（多机安全的地基）。pactify 侧已把客户端写成**向前兼容**（现状能跑，relay 加了之后自动更安全）。

### B1. 服务端挑战 nonce（防重放，spec §6.1 / A1）
- **现状**：`POST /v1/auth` 的 challenge 由**客户端生成**（`cloudclient`/`relay-client` 都是随机 UUID）→ 理论上可重放。
- **要 linx 做**：relay 加 `POST /v1/auth/challenge` 发一次性 server nonce（短 TTL、单次），客户端签它;`/v1/auth` 校验 nonce 未用过 + 未过期。
- **pactify 侧兼容**：M2 会让 `cloudclient.Authenticate` **优先请求 server nonce,拿不到(旧 relay)回退客户端 nonce**——所以 pactify 先上线不会破;linx 加了端点后双方都走 server nonce。**约定：端点路径 `/v1/auth/challenge`,返回 `{nonce, expMs}`,`/v1/auth` body 增加可选 `nonce` 字段。**

### B2. 机器预置校验（防同账户内冒充 machineId，A2/A4）
- **现状**：socket 以 `role=machine` 连接时,`machineId` 取自 handshake auth,relay **不校验该 machineId 是否属于该账户已注册/配对的机器** → 同账户内一个 socket 可冒充任意 machineId（多机场景下 A 机可冒充 B 机接收派给 B 的 rpc）。
- **要 linx 做**：machine socket 连接时,校验 `machineId` ∈ 该账户**已 pair 的机器集合**(或首次连接走一次显式 pair 确认后才允许)。未通过 → 拒绝连接。
- **pactify 侧兼容**：M2 提供 `pactify account pair-machine`（把本机 machineId 注册到账户的已配对集合;经现有 pairing 通道）。relay 未强制前,注册是幂等 no-op;relay 强制后成为前置。**约定：沿用现有 pairing 机制,machine 注册进 `Machine` 表即视为已配对(relay 校验 `Machine` 行存在且 accountId 匹配)。**

### B3.（可选，M2）pairing 密钥确认回合（C3）
- 现状 pairing 是单向;完整的 relay-blind 主动 relay 需要密钥确认回合。**非阻塞**——M2 的机器预置用 B2 的「Machine 行存在」判据即可,C3 是后续加固。

---

## C. 无需 linx 改动、但告知的项
- **register/machines/心跳/离线清理**：linx 已建,pactify M1 直接复用,**零改动**。
- **rpc 明文头**：pactify 的 `pact.*`/`orchestrate.*` rpc 头(project/task 名)经 relay 明文,与 U2 明文操作头哲学一致;敏感内容(spec/代码)走 git,不进 rpc。M5 评估是否 E2E 化 briefing。
- **相同的数据健壮性**：pactify 修过的 ingest 并发/幂等(#9)linx 已同步;后续若 pactify 再动共享 relay 数据层,会在本文件追加。

---

## D. 协调窗口建议
- **B1+B2 是一次 relay 重部署可一起上**的（都在 socket auth / /v1/auth 路径）。建议约一个窗口:linx 加 B1 端点 + B2 校验 → relay 重部署 staging → pactify 侧已兼容,直接验证 → 通过后 prod。
- A 类 wire 追加是 pactify 每次上线时 bump wire 版本 + relay 重部署即可,不需要专门窗口(additive)。

**pactify 侧进度**：见 `docs/backlog.md` 的 M1–M5 条目 + `.agent/plans/multi-machine-orchestration-2026-07-04.md`。本文件随 M2–M5 推进持续更新。
