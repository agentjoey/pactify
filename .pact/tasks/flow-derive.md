# Task flow-derive (Flow F1-a) — deriveFlow 纯函数:账本事件 → 泳道模型

## 目标(设计稿 .agent/plans/agent-flow-view-design-2026-07-10.md §2/§3)
新增 `web/src/lib/flowderive.ts`:**纯函数**把 PactEvent[](types.ts:5,字段
event_id/ts(RFC3339)/agent_id/role/event_type/task_id/feature/payload)推导成三个渲染器
(泳道/会话流/办公室)共用的模型。零后端改动。

## 输出模型(导出这些类型)
```ts
export interface FlowLane { id: string; firstT: number }            // 座席,按首次活动排序(orchestrator 角色置顶)
export interface FlowStint { agent: string; task: string; kind: "work"|"rework"|"review"; t0: number; t1: number|null } // t1=null 表示进行中
export interface FlowArrow { verb: "assign"|"checkpoint"|"changes"|"accept"; from: string; to: string; task: string; t: number; note?: string }
export interface FlowMark  { agent: string; verb: "merge"|"join"; task?: string; t: number }
export interface FlowGap   { t0: number; t1: number }               // 被压缩的空档(真实毫秒区间)
export interface FlowModel {
  lanes: FlowLane[]; stints: FlowStint[]; arrows: FlowArrow[]; marks: FlowMark[];
  gaps: FlowGap[]; tMin: number; tMax: number;                      // 毫秒
  x(t: number): number;                                             // 毫秒→[0,1] 压缩后归一化位置
}
export function deriveFlow(events: PactEvent[], opts?: { gapMinMs?: number }): FlowModel
```

## 推导规则(账本语义,全部可从 events 单独推出)
- **时间**:ts 解析为毫秒;无效 ts 的事件跳过。事件按 ts 升序处理(输入可能乱序,先排序)。
- **lanes**:出现过的 agent_id 全收;排序:role=="orchestrator" 的事件的 agent 置顶(首个),
  其余按 firstT 升序。
- **arrows**(跨座席动词;from/to):
  - assign:from=事件 agent_id,to=payload.owner(string;缺则跳过箭头但仍记 stint 起点见下)。
  - checkpoint:from=agent_id(owner),to=该 task 的 reviewer——从此前该 task 的 assign payload.reviewer 取;
    若 payload.reviewers(数组,quorum)存在则**每个 reviewer 一条箭头**。找不到 reviewer → 跳过箭头。
  - changes_requested → verb:"changes":from=agent_id(reviewer),to=task owner(assign payload.owner)。
  - accept:from=agent_id,to=owner。
- **marks**:merge(agent_id,task_id 可空,feature 在 note?不需要)、join(agent_id)。
- **stints**(状态机,per task):
  - assign(或 changes 之后)→ owner 的 work/rework stint 开始(assign 后首个=work;changes 后=rework);
    该 owner 的 checkpoint 结束它(t1=checkpoint ts)。
  - checkpoint → 每个 reviewer 一段 review stint 开始;该 reviewer 对该 task 的 accept/changes 结束它。
    **同一 reviewer 忙别的 review 时不建模排队**(v1 简化:开始时间=checkpoint ts;真实排队表现为重叠,可接受)。
  - 未结束的 stint t1=null(进行中)。
- **gaps 与 x()**:找相邻事件间隔 > gapMinMs(默认 30*60_000)的区间记入 gaps;
  x(t) 把每个 gap 压缩为固定小宽度(总时长扣除 gap 真实长、每个 gap 记 GAP_W=2% 归一宽),
  线性映射其余区段到 [0,1]。x 单调不减;t<tMin→0,t>tMax→1。
- 空输入:返回空模型(lanes/stints 等为 []),x 恒 0,不 throw。

## 测试(web/src/lib/flowderive.test.ts,vitest,构造真实形状事件)
- 完整 t1 剧本(init 不需要;join→assign→checkpoint→changes→checkpoint(reviewers 两人)→accept×2→merge):
  断言 lanes 排序(orchestrator 置顶)、work+rework+review stints 区间正确、quorum 出两条 checkpoint 箭头、
  changes 箭头 from/to 对、进行中 stint t1===null(去掉尾部 accept 再测)。
- 乱序输入(shuffle)结果与有序一致。
- gap:两事件相隔 2h → gaps 有一条;x(gap 前)<x(gap 后),且 gap 区间宽度≈GAP_W。
- 无效 ts / 缺 payload.owner 的 assign:不 throw,箭头跳过。

## 验收(视角: correctness — 语义与账本一致、纯函数无副作用、边界不炸)
verify: cd web && npx tsc --noEmit && npx vitest run src/lib/flowderive.test.ts
