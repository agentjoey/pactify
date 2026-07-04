# fe-machines：托管侧「Machines」视图（多机在线+agent 清单）（M1 前端）

> 多机编排 M1：让 web 看见同账号下的机器。`@pactify-apps/relay-client` 已加 `listMachines(): Promise<MachineInfo[]>`（GET /v1/machines）。`MachineInfo = { machineId, host?, agentKinds: string[], workdirs?: string[], online: boolean, lastSeenAt: number }`。先读 `web/src/lib/relaysource.ts`、`web/src/lib/datasource.tsx`、`web/src/lib/types.ts`、`web/src/components/`（现有列表/面板样式，dark UI）。

## 目标
托管模式下展示账号的机器 roster：每台机器的 host、在线状态、可驱动的 agent 种类。为后续（M3 指定机器/agent）打基础，本任务**只读展示**。

1. **类型**：`web/src/lib/types.ts` 加 `Machine`（对齐 MachineInfo：machineId/host/agentKinds/workdirs?/online/lastSeenAt）。
2. **DataSource**：`datasource.tsx` 接口加**可选** `getMachines?(): Promise<Machine[]>`。`LocalServeSource` 不实现（本地无 relay 机器概念，返回 undefined/不实现即可）。
3. **RelaySource**：`relaysource.ts` 加 `getMachines(): Promise<Machine[]>` = `this.client.listMachines()` 映射成 `Machine[]`。
4. **Machines 视图组件**（`web/src/components/Machines.tsx` 新 + test）：调 `src.getMachines?.()`，渲染机器列表——每行 host（无则 machineId 前 8 位）+ 在线灯（online 绿/离线灰）+ agent kinds 徽标簇 + lastSeenAt 相对时间。空/加载/无 getMachines（本地）优雅降级（本地不显示该视图或显示「本地模式」提示）。
5. **接线**：挂到一个合理位置——建议托管模式下 header 或 Settings 里一个「Machines」入口/区块（不破坏现有 Board/Live/Canvas 三视图）。最小侵入，别改现有视图切换逻辑。

## 文件
- 改 `web/src/lib/types.ts`、`web/src/lib/datasource.tsx`、`web/src/lib/relaysource.ts`
- 建 `web/src/components/Machines.tsx` + `Machines.test.tsx`
- 接线处（App.tsx 或 shell，最小改动）
- `web/src/lib/relaysource.test.ts` 补 getMachines 测试（mock client.listMachines）

## 纪律
- **本地零回归**：本地不实现 getMachines 不报错。
- 复用现有 dark UI 列表/徽标样式，别造新风格。
- 测试：RelaySource.getMachines（mock 返回机器→断言映射）、Machines 组件（mock 数据渲染在线/离线/agentKinds）。
- 零回归：tsc + 全量 vitest + playwright 全绿。

verify: cd web && npx tsc -b && npx vitest run && npx playwright test
