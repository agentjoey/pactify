---
id: phase0-c1
feature: phase0-sec
owner: kimi-worker
reviewer: claude
---

# phase0-c1 — 修复 C1：机器定向 rpc + socket 握手无归属校验 → 跨租户 RCE/窃听（critical）

## 目标 / Goal
`sockets.ts` 的 `rpc` handler 机器定向分支把消息推到 `machine:${rpc.machineId}` 房间时**不校验该机器属于发送方账号**（紧邻的 run 分支却校验 `run.accountId === accountId`）；且 `io.use` 接受任意带 machineId 的合法 token、从不校验归属。于是任一认证账号可 (1) 向别账号的机器注入 rpc（spawn/pact.*）= 攻击者定向代码执行，(2) 以别账号 machineId 连接、join 其房间窃听明文控制载荷。

## 改文件 / Files（只碰这个）
- `cloud/relay/src/sockets.ts`（`rpc` handler 机器定向分支 + `io.use` 握手）

## 契约 / Contract
1. **【必做】rpc 路由归属校验**：机器定向分支 push `machine:${rpc.machineId}` 之前，查归属——
   ```ts
   const m = await db.machine.findFirst({ where: { id: rpc.machineId, accountId } })
   if (!m) { socket.emit('rpc-error', { error: 'unknown machine' }); return }
   targetRooms.push(`machine:${m.id}`)
   ```
   对齐紧邻 run 分支的 `run.accountId === accountId` 纪律。（同账号定向自己的机器仍照常路由——见既有测试 :103。）
2. **【必做】io.use 握手归属校验**：`role === 'machine'` 且带 `machineId` 时，查既存机器归属：
   ```ts
   if (auth.role === 'machine' && auth.machineId) {
     const existing = await db.machine.findUnique({ where: { id: auth.machineId } })
     if (existing && existing.accountId !== v.accountId) {
       return next(new Error('machine not owned by account'))
     }
   }
   ```
   ⚠️ **必须允许**：`machineId` 在库中不存在（首次注册的新机器）或属于本账号——**别破坏机器首次上线注册**（既有测试 :77/:103 用 role=machine, machineId='m1' 连接、m1 属于本账号，须仍能连）。io.use 因这个 await 变 async（await 后再 `next()`）。

## 验收 / Acceptance（dimension: security）
- `cd cloud/relay && npx vitest run test/sockets.test.ts` —— C1 两条测试**转绿**（现红），且**既有 sockets 测试全部不破**（尤其 :77 machine 连接 + ingest、:103 same-account machine rpc 路由）。
- `npx tsc -b --noEmit` 干净。

verify: cd cloud/relay && npx vitest run test/sockets.test.ts
