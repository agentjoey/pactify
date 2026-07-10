# Task hc-rpc (E3-2) — cockpit 远程 RPC(下行)+ ephemeral courier 事件流(上行)

## 背景(读完再动手)
- 下行既有轨道:web `sendRpc` → relay 校验 `RpcRequest`(cloud/wire rpc.ts,有界 union)→
  `io.to(machine:<id>)` → 机器 `internal/remotemachine` → `internal/remoteexec.Dispatcher.Handle`。
- 上行既有轨道:relay `sockets.ts` 的 ingest 处理里,`header.ephemeral=true` 的 WireMessage
  **不落库、直接转发 `acct:<accountId>` 房间**(courier)。机器沿既有 socket emit `ingest`
  (payload=`{agentKind, msg:{header:OperationalHeader, body:EncryptedBlob}}`,zod strict)即可,
  **relay 上行方向零改动**。
- 加密:`internal/serve/relay.go` 的 projectKey/buildBody 已有按项目派生密钥 + EncryptedBlob
  容器;web `decryptRaw(projectId, bodyEnc)` 同派生。复用(需要就小幅导出/搬 helper,别复制实现)。

## 交付
### 1. cloud/wire 下行 RPC 类型(additive)
rpc.ts 加(全部带 machineId+project+seat,照 PactStintRequest 风格与注释规约):
- `cockpit.prompt` {text}
- `cockpit.permission` {requestId, decision: 'allow'|'deny'|'allow_session'}
- `cockpit.cancel` {}
- `cockpit.resume` {}
- `cockpit.subscribe` {}(订阅=打开该 (project,seat) 会话事件上行镜像,TTL 5min,重发续期)
加入 RpcRequest union;`npm run build` 或包内既有校验方式过。linx 忽略未知类型,additive 安全。

### 2. Go 侧 dispatcher(internal/remoteexec)
- 新 `Cockpiter` interface(照 Stinter 先例):Prompt/Permission/Cancel/Resume/Subscribe。
- dispatch.go Handle 路由 `cockpit.*` → Cockpiter,**每个都先过 per-project RemotePolicy.Cockpit
  门**(照 stint 的 readRemotePolicy 模式;false → Reply{OK:false,"remote cockpit not enabled…"})。
- 单测:fake Cockpiter,门开/关、account scope、未知 project 全覆盖。

### 3. serve 侧 Cockpiter 实现 + 上行镜像(internal/serve)
- 实现 Cockpiter:桥到 cockpit.Manager(prompt/permission/cancel/resume 语义与 HTTP 端点一致,
  复用同一段逻辑,别复制 handler 内容——抽共享函数)。
- Subscribe:给该 session 挂一个 remote mirror consumer:每条 cockpit 事件(含 approval
  request/resolve)→ 构造 WireMessage{header:{runId:"cockpit:<project>:<seat>", seq:递增,
  ephemeral:true}, body:projectKey 加密的事件 JSON} → 沿机器 socket emit "ingest"。
  agentKind 用座席 kind 映射到 wire AgentKind 枚举(claude-code→claude 等;兜底 claude)。
  订阅 TTL 5min 到期即停(subscribe 重发续期);session 到期/关闭即停。
  首次订阅先发一条 snapshot 事件(kind "status":pending 列表+resumable+threadId)再流后续。
- remotechannel.go 把 Cockpiter 接进 Dispatcher(照 stint 接法)。
- 单测:fake socket 收 emit;订阅→prompt→事件镜像到 fake socket、TTL 到期停、策略门关不 emit。

## 门
go test ./... 相关包全绿 + go vet;cloud/wire 构建绿。不碰 web/src(T3 做)。
verify: go test ./internal/remoteexec/... ./internal/serve/... ./internal/cockpit/... && go vet ./... && cd cloud/wire && npm run build 2>/dev/null || npx tsc --noEmit
