# m0-relay：迁入 @pactify/relay（含 testing 子路径与部署文件改写）

> 第三棒（依赖 m0-crypto 已 accepted）。把 relay 服务从 linx 迁入 cloud/，
> 改名 @pactify/relay，并补一个 `/testing` 子路径导出（linx 仓的测试要靠它活）。

## 源仓（只读！）
`~/AgentWorks/CodeSpace/pactify-apps/linx` — **绝对不许改动源仓任何文件**。

## 目标
1. 把源仓 `packages/linx-relay/` 整目录复制为 `cloud/relay/`（src/、prisma/、bin/、测试、
   Dockerfile、tsconfig、package.json 全部）。
2. `cloud/relay/package.json` 允许的改动（其余逐字保留）：
   - `"name": "@pactify/relay"`, `"version": "0.1.0"`；删 `"private": true`；
     加 `"publishConfig": {"access":"public"}`；
   - `"bin"`：`"linx-relay"` 改 `"pactify-relay"`（指向文件不变，必要时同步重命名 bin/ 下文件）；
   - 依赖 `"@pactify/wire": "workspace:*"` 保持 workspace 形态；
   - 新增 `"exports"` 映射：`"."` 保持原 main/types；`"./testing"` 指向新建的
     `src/testing.ts` 构建产物（types + import 双字段）。确保 `"files"` 覆盖 dist。
3. 新写 `cloud/relay/src/testing.ts`（唯一的新源码）：re-export `createPgliteDb`、
   `createServer`、`attachSockets`、`issueToken`（都在现有 src/ 模块里，自己定位准确来源）。
4. Dockerfile 改写：源版假设 linx monorepo 布局（COPY packages/wire packages/linx-relay）。
   改为以 `cloud/` 为构建上下文：COPY wire/ relay/ + cloud 根的 workspace 文件；
   多阶段结构、node:24-slim 基底、`pnpm prisma migrate deploy && node dist/server-main.js`
   启动令均保持。文件顶部加一行注释注明构建上下文（`fly deploy` 时 `--dockerfile relay/Dockerfile`
   以 cloud/ 为 context）。**不要求本机 docker build 验证**（review 轮 + 实际部署时验）。
5. Fly 配置：源仓根有 `fly.toml`（app = pactify-linx-relay）。在 `cloud/relay/` 新建：
   - `fly.toml`：`app = "pactify-relay"`，其余（sin 区、PORT 4310、force_https、
     auto_stop_machines off、min_machines_running 1、shared-cpu-1x/512mb）逐字照搬源版；
     dockerfile 路径按新布局改。
   - `fly.staging.toml`：同上但 `app = "pactify-relay-staging"`。
6. prisma/ 目录原样照抄（schema 一字不动——同一 Neon 库，schema 归属随迁但内容不变）。

## 纪律（迁移保真 — 最高优先级）
- src/ 与测试逐字节照抄（import 路径/包名必须改的除外，逐条列 evidence）。
- 疑似 bug 只记录不修。
- 测试用 PGlite 内存库，不需要外部 DATABASE_URL；如有测试因环境缺失失败，先查是不是照抄遗漏，
  确非代码问题再在 evidence 说明。
- 自查：`diff -r <源仓>/packages/linx-relay/src cloud/relay/src`（排除新增 testing.ts），
  diff 结果贴 evidence。

verify: cd cloud && pnpm install && pnpm -r build && pnpm --filter @pactify/relay test
