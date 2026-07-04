# fe-apihygiene：api.ts mutation 统一走 writeJSON（错误提取一致）（FE-12 子项）

> 来源 backlog FE-12 / code-review-2026-07-02 §108/§113。先读 `web/src/lib/api.ts` 的 `writeJSON` helper 与各 mutation 函数。

## 问题
`api.ts` 里个别 mutation 是**裸 `fetch`**、不走共享的 `writeJSON`，因此不做统一的错误提取（HTTP 错误体→可读 message），行为不一致、错误静默。已知：`deleteManifest`（api.ts:428 附近）。

## 目标
把所有写操作（POST/PUT/DELETE）统一走 `writeJSON`（或等价的共享错误提取路径），错误信息一致可读。

1. **审计**：通读 `api.ts`，找出所有直接用 `fetch(...)` 做 mutation 但**没走 `writeJSON`** 的函数（至少 `deleteManifest`；确认是否还有别的，如 createManifest 重复错误处理逻辑 §113）。
2. **改**：这些函数改走 `writeJSON`（或抽出的共享 `extractErrorMessage`），删掉手抄的错误处理。保持函数签名与返回类型不变（调用方零改）。
3. **测试**：`api.test.ts` 加/改覆盖：mutation 失败时抛出带服务端 message 的错误（mock 一个 4xx JSON 错误体，断言抛出的 error message 含服务端文案）。

## 文件
- 改 `web/src/lib/api.ts`
- 改/加 `web/src/lib/api.test.ts`
- 不改调用方组件（签名不变）

## 纪律
- **签名/返回不变**：只改内部实现走 writeJSON，调用方无感。
- 不引入行为变化（除了错误信息更好）。全量 vitest + tsc 保持绿。
- 完成跑 verify 绿再 checkpoint。

verify: cd web && npx tsc --noEmit && npx vitest run src/lib
