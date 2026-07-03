# proj-fixture：Go 黄金夹具导出器（events→StateDTO，跨语言 parity 契约）

> 设计见 `.agent/plans/sprint-p1p2-foundation-2026-07-04.md` §契约。
> 先读 `internal/serve/dto.go`（`StateDTO` / `toDTO(projection.State)` / `ProjectState`）、`internal/projection/project.go`（fold 逻辑）、`internal/event`（事件类型）。

## 目标
产出一份**跨语言 parity 黄金夹具**：一组代表性 pact 事件 + 其折叠出的 `StateDTO` JSON。下一棒 `pact-project`（TS）用它钉死「TS 投影 == Go 投影」。

1. **代表性事件集**：覆盖 init/add-seat、assign（含 deps）、checkpoint（→awaiting_review）、accept、changes（→重回 in_progress/assigned）、merge（→feature shipped）、多 feature、多 task 的组合。要能触发 `awaiting_count>0`。以真实 `.pact/log.jsonl` 事件格式书写。
2. **导出器**：`internal/serve/` 加一个测试（如 `fixture_export_test.go`，`TestFixtureExport`）——把该事件集喂现有折叠路径（`projection.Project` → `toDTO`，或复用 `ProjectState` 的等价内存路径）得到 `StateDTO`，序列化为**稳定 JSON**（字段序确定、无时间戳漂移），断言等于 committed 的黄金文件；`-update` 或环境开关下可重写黄金文件（TDD 友好）。
3. **落盘位置**（供 TS 侧读）：
   - `cloud/pact-project/testdata/events-golden.jsonl`（输入事件，一行一事件）
   - `cloud/pact-project/testdata/state-golden.json`（期望 StateDTO，pretty JSON）
   （目录先建；pact-project 包在下一棒创建，夹具先到位无碍。）

## 文件
- 建 `internal/serve/fixture_export_test.go`
- 建 `cloud/pact-project/testdata/events-golden.jsonl` + `state-golden.json`
- 若需共享构造，允许小工具函数（不改现有生产逻辑）

## 纪律
- **不改现有生产代码**：只加测试 + testdata。折叠必须走**现有** projection/toDTO 路径（不另起炉灶，否则 parity 失去意义）。
- JSON 必须**稳定可复现**（json.MarshalIndent + 确定字段序；State 无 map 遍历不确定性——若有，排序）。
- 夹具要**够刁**：deps、awaiting_review、changes 回退、merge shipped 都覆盖，否则 TS 侧漏 case 也能假绿。
- 完成跑 verify 绿再 checkpoint。

verify: go test ./internal/serve/ -run Fixture
