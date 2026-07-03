# pact-project：@pactify-apps/pact-project TS 投影包 + Go parity（P2）

> 依赖 proj-fixture（`cloud/pact-project/testdata/{events-golden.jsonl,state-golden.json}` 已就绪）。
> 设计见 `.agent/plans/product-form-and-frontend-2026-07-04.md` §2.2。
> 先读夹具、`internal/projection/project.go`（要复现的 fold 语义）、`internal/serve/dto.go`（awaiting_count 规则）、`cloud/wire` 或 `cloud/crypto` 的 package.json/tsconfig 作**新包脚手架样板**。

## 目标
新增 cloud workspace 包 `@pactify-apps/pact-project`：纯函数 `project(events) => State`，逐字段复现 Go 折叠；parity 测试对黄金夹具深等。

1. **包脚手架**（照 `cloud/wire` 的 package.json/tsconfig.json/vitest 结构）：
   - `cloud/pact-project/package.json`：name `@pactify-apps/pact-project`、version `0.1.0`、`publishConfig.access=public`（与 wire/crypto 一致的公开发布姿态）、exports 主入口、build=tsc、test=vitest。
   - `cloud/pact-project/tsconfig.json`：extends `../tsconfig.base.json`。
2. **投影**：`src/index.ts` 导出 `project(events: PactEvent[]): State`（+ State/Feature/Task/Seat TS 类型，与 `web/src/lib/types.ts` 及 Go `StateDTO` 对齐，含 `awaiting_count`）。逐条 fold 事件复现 Go 语义：init/add-seat 建 seats、assign 建 task（deps 仅在非空时带）、checkpoint→awaiting_review、accept→accepted、changes→回退、merge→feature shipped、awaiting_count 累加 awaiting_review 任务。
   - 事件输入类型：定义 `PactEvent`（与夹具 jsonl 行结构一致；可从夹具反推字段）。
3. **parity 测试**：`test/parity.test.ts` 读 `testdata/events-golden.jsonl` → `project(...)` → 深等 `testdata/state-golden.json`。**这是本包的核心验收**。另加若干单元测试覆盖单事件语义。

## 文件
- 建 `cloud/pact-project/`：package.json、tsconfig.json、vitest.config.ts、src/index.ts、test/parity.test.ts（+ 单元测试）
- 复用 proj-fixture 落的 testdata（勿改夹具；夹具是契约）
- 更新 `cloud/pnpm-lock.yaml`（pnpm install 自然产生）

## 纪律
- **parity 是硬门**：`project(events-golden)` 必须逐字段深等 `state-golden.json`。不为凑绿去改夹具——夹具错该退回 proj-fixture 改。
- 纯函数、无副作用、无框架依赖（不 import react/fastify）。
- 复现 Go 语义要**读 project.go 对齐**，不臆测（deps 省略规则、changes 回退目标状态、shipped 归属都照 Go）。
- 完成跑 verify 绿再 checkpoint。

verify: cd cloud && pnpm --filter @pactify-apps/pact-project build && pnpm --filter @pactify-apps/pact-project test
