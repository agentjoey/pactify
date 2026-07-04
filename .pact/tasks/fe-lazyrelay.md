# fe-lazyrelay：本地模式不加载 crypto-relay 块（RelaySource 懒加载）（B / FE-11c）

> 承接 fe-codesplit（已把 crypto+relay-client+noble+socket 拆成 `crypto-relay` 158kB 独立块）。但 `web/src/lib/source.ts` **静态 import** `RelaySource`/`RelayClient`,导致**本地模式(serve 内嵌)也下载 crypto-relay 块**——本地根本用不到它(只有 hosted 模式解密 relay)。先读 `web/src/lib/source.ts`、`web/src/App.tsx`(App() 里 source 选择)、`web/src/components/RelayConnect.tsx`。

## 目标
让 `crypto-relay` 块**只在 hosted 模式按需加载**;本地模式的 bundle 完全不含它。**零功能变化**:hosted 连接、本地 board 都照常。

1. **`source.ts` 改动态 import**：`connectRelaySource` 内部 `const { RelaySource } = await import("./relaysource")` + `const { RelayClient } = await import("@pactify-apps/relay-client")`（动态）,不再顶层静态 import 它们。`hexToBytes`/env 逻辑保持。这样 `relaysource`/`relay-client`/`crypto` 只在调用 `connectRelaySource`(即 hosted 用户点 Connect)时才加载。
2. **RelayConnect.tsx** 已经 async 调 `connectRelaySource`——确认改动后它仍编译(类型上 `connectRelaySource` 仍返回 `Promise<RelaySource>`,可能要把 RelaySource 类型 import 保留为 `import type`(type-only 不进 bundle))。
3. **App.tsx**：`localSource()` 保持同步(LocalServeSource 静态,本地立即可用);hosted 分支不变。
4. **验证 chunk**:本地 build(`npm run build`,无 env)后 **crypto-relay 块不应出现或不被主/入口静态引用**;hosted build(`VITE_PACTIFY_RELAY_URL=... npm run build`)仍产出并按需加载它。

## 文件
- 改 `web/src/lib/source.ts`（动态 import RelaySource/RelayClient）
- 如需:`web/src/components/RelayConnect.tsx`(type-only import RelaySource)、`web/src/lib/source.test.ts`（动态 import 后测试仍绿）
- 重建 `internal/serve/dist`（本地嵌入版）并提交

## 纪律
- **零功能变化**：hosted Connect 流程、本地 board 都不变。source.test + RelayConnect.test 全绿。
- `import type { RelaySource }` 只做类型(不触发 bundle 包含)。
- **零回归**:tsc + 全量 vitest + playwright 全绿。
- evidence 贴本地 build 的 chunk 列表(证明 crypto-relay 不再静态进本地 bundle)。

verify: cd web && npx tsc -b && npx vitest run && npx playwright test
