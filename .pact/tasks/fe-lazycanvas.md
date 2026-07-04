# fe-lazycanvas：Canvas 视图懒加载（延续 code-split，Canvas 块不进首屏）（B / FE-11b）

> 承接已合入的 `fe-codesplit`（manualChunks 已把 xyflow 拆成 175kB 独立块）。现在 Canvas 块虽独立但仍随首屏静态加载。先读 `web/src/App.tsx`（视图切换 + Canvas 挂载处）、`web/src/components/`（Canvas 组件）、`web/vite.config.ts`。

## 目标
把 **Canvas 视图**改成 `React.lazy` + `Suspense` 动态 import，**只看 Board/Live 的用户不下载 xyflow 块**（首屏更快）。**零功能变化**。

1. **定位 Canvas 视图组件**（App.tsx 里 `view==="canvas"` 渲染的那个顶层组件，用 `@xyflow/react`）。
2. **改静态 import 为 `const Canvas = React.lazy(() => import("...Canvas..."))`**，在渲染处用 `<Suspense fallback={<Spinner … />}>` 包裹（fallback 用现有 `ui/Spinner`，避免闪白）。
3. 确认 build 后 xyflow 块变成**按需异步 chunk**（不在主入口的静态依赖里）；切到 Canvas 视图时才加载。
4. **重建 `internal/serve/dist`** 并提交。

## 文件
- 改 `web/src/App.tsx`（Canvas 视图 lazy + Suspense）
- 相应更新受影响测试（App.test 里切 Canvas 视图的用例需容忍异步加载——用 `findBy*`/`waitFor`）
- 不改 Canvas 组件内部、不改其它视图

## 纪律
- **零功能变化**：切到 Canvas 视图仍正常渲染（playwright 的 canvas/office e2e 若存在必须绿）。
- Suspense fallback 必给（Spinner），避免加载空白。
- **零回归**：tsc + 全量 vitest + playwright 全绿。
- 完成把 build 的 chunk 列表贴进 evidence（证明 xyflow 变按需块）。

verify: cd web && npx tsc -b && npx vitest run && npx playwright test
