# m0-crypto：从 linx core 抽出 @pactify/crypto

> 第二棒（依赖 m0-wire 已 accepted，`cloud/` workspace 已存在）。
> 把账户层密码学从 linx 的 core 包抽成独立共享包。

## 源仓（只读！）
`/Users/xtation/AgentWorks/CodeSpace/pactify-apps/linx` — **绝对不许改动源仓任何文件**。

## 目标
1. 新建 `cloud/crypto/` 包，`package.json`：
   - `"name": "@pactify/crypto"`, `"version": "0.1.0"`, `"publishConfig": {"access":"public"}`,
     `"files": ["dist"]`；main/types/exports 形态照抄 cloud/wire 的模式。
   - dependencies：`"@pactify/wire": "workspace:*"` + 从源仓 `packages/core/package.json` 里
     **原版本号**照搬这三个文件实际用到的库（@noble/ciphers、@noble/curves、@noble/hashes、
     @scure/base 等，以实际 import 为准）。devDependencies 搬测试工具链（同版本）。
2. 从源仓 `packages/core/src/` 复制这三个文件到 `cloud/crypto/src/`：
   - `keys.ts`（deriveAccountKeypair / deriveRunKey / generateMasterSecret）
   - `crypto.ts`（信封 encrypt/decryptEvent 等）
   - `pairing.ts`（配对握手）
   同时找到源仓 core 包里**覆盖这三个模块的测试文件**（可能在 src/ 同级 `*.test.ts` 或 test/ 目录，
   自己定位），一并复制过来并保证能跑。
3. 新写 `cloud/crypto/src/index.ts` barrel（这是唯一的新代码）：
   `export * from './keys.js'` + `'./crypto.js'` + `'./pairing.js'`。
4. tsconfig 照抄 cloud/wire 的模式（或源仓 core 的，以能编译为准）。

## 边界检查（先做再动手）
- 逐一检查三个文件的 import：只允许依赖彼此、`@pactify/wire`、noble/scure 系和 node 内置。
  **如果发现它们 import 了 core 的其他模块**（如 backend/run-assembler），停下来，把依赖关系
  写进 checkpoint evidence 并说明，不要擅自把更多 core 文件拖进来——那属于设计变更。
- 不要碰 master-secret 文件路径相关代码（在 linx-cli 包里，不迁）。

## 纪律（迁移保真 — 最高优先级）
- 三个 .ts 文件与测试**逐字节照抄**（import 路径因包边界变化必须改的除外，逐条列进 evidence）。
- 疑似 bug 只记录不修；任何偏离照抄之处 evidence 里逐条列出。
- 自查：对每个迁移文件 `diff <源仓路径> <新路径>`，把 diff（应只剩 import 行差异）贴进 evidence。

verify: cd cloud && pnpm install && pnpm -r build && pnpm -r test
