# fe-remote-orchestrate：托管侧远程编排 UI（选机器起 orchestrate）（M4 前端）

> 多机编排 M3 已完成：`orchestrate.run/resume` rpc 可远程起某台机器的 driver；`pact.stint` 可派棒。现在给托管 web 接上入口。先读 `web/src/lib/relaysource.ts`（verb/sendRpc/getMachines/setTargetMachine 模式）、`web/src/lib/datasource.tsx`（DataSource 能力）、`web/src/components/Machines.tsx`（M1 机器视图）、`web/src/components/LiveOrchestrate.tsx`（canOrchestrate 门控处）、`@pactify-apps/wire` 的 `OrchestrateRunRequest/OrchestrateResumeRequest`（rpc 形状：type/machineId/project/feature?/seatKinds?）。

## 目标
托管模式下能**选一台在线机器远程起 orchestrate**。

1. **RelaySource 加 orchestrate 方法**：
   - `runOrchestrate(project, body?)`：构造 `{type:'orchestrate.run', machineId, project, feature?, seatKinds?}` rpc → `client.sendRpc`（machineId 用 `resolveMachineId()` 现有逻辑：pinned 或首台在线）。返回 `{status_url: ""}`（fire-and-forget，效果经事件流回）。
   - `resumeOrchestrate(project, body?)` 同理（type: