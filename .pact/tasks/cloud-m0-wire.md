# m0-wire：建 cloud/ workspace + 迁入 @pactify/wire

> 背景：wire/crypto/relay 三包的代码归属从 linx 仓迁入本仓 `cloud/` workspace（详见
> `.agent/plans/shared-architecture-2026-07-03.md`，看不到该文件也不影响本任务执行）。
> 本任务是第一棒：搭 workspace 骨架 + 迁 wire。

## 源仓（只读！）
`/Users/xtation/AgentWorks/CodeSpace/pactify-apps/linx` — **绝对不许改动源仓任何文件**，只读复制。

## 目标
1. 新建 `cloud/` pnpm workspace 骨架：
   - `cloud/pnpm-workspace.yaml`：`packages: ["*"]`
   - `cloud/package.json`：`"name": "@pactify/cloud-root"`, `"private": true`，scripts：
     `"build": "pnpm -r build"`, `"test": "pnpm -r test"`。若 linx 根 `package.json` 的
     devDependencies 里有 wire 构建/测试所需的共享工具链（typescript、vitest 等），按**相同版本**
     搬到 cloud 根或 cloud/wire 的 devDependencies（哪边缺放哪边，以 verify 通过为准）。
   - `cloud/.gitignore`：`node_modules/`、`dist/`、`*.tsbuildinfo`。
   - 注意：本仓根目录**没有** JS workspace，cloud/ 自包含，不要动根目录或 web/、site/ 的任何配置。
2. 迁入 wire：把源仓 `packages/wire/` 整目录复制为 `cloud/wire/`（src/ 全部、测试、tsconfig、
   package.json）。
3. `cloud/wire/package.json` 仅允许以下改动（其余字段逐字保留）：
   - `"version": "0.1.0"`；删除 `"private": true`；
   - 加 `"publishConfig": { "access": "public" }`；
   - 加 `"files": ["dist"]`（若原本没有）；
   - 依赖里若有 `workspace:*` 引用保持 `workspace:*` 形态。
4. `pnpm install` 会生成 `cloud/pnpm-lock.yaml`，提交它。

## 纪律（迁移保真 — 最高优先级）
- **src/ 与测试文件逐字节照抄，零语义改动**。不重排 import、不改格式、不"顺手优化"。
- 发现疑似 bug 只记录在 checkpoint evidence 里（"发现待议：…"），**不修**——后续有专门 review 轮。
- 任何偏离照抄的地方（哪怕一行）必须在 evidence 中逐条列出并给原因。
- 完成后自查：`diff -r <源仓>/packages/wire/src cloud/wire/src` 应为空，把该 diff 结果贴进 evidence。

verify: cd cloud && pnpm install && pnpm -r build && pnpm -r test
