# fe-codesplit：拆分托管 bundle（现单块 896kB，Vercel 已告警）（B / FE-11）

> 托管 React 版已上线（orx.pactify.dev），但生产 bundle 是单块 ~896kB（gzip 261kB），Vercel 构建告警「chunks larger than 500 kB」。先读 `web/vite.config.ts`（build 段）。

## 目标
把生产 bundle 拆成合理的 chunk，主入口显著变小、首屏更快，**不改任何功能**。

1. **vendor 分块**（`web/vite.config.ts` 的 `build.rollupOptions.output.manualChunks`）：把大依赖各自成块——至少 `react`/`react-dom` 一块、`@xyflow/react`（Canvas，最大）一块、`@pactify-apps/crypto` + noble/scure + `socket.io-client`（托管解密/relay，仅 RelaySource 用）一块。目标：主 index chunk 明显下降，Canvas/crypto 不再压进主块。
2. **（可选，若稳妥）** 路由级懒加载：Canvas 视图用 `React.lazy` + `Suspense` 动态 import，让只看 Board 的用户不下载 Canvas 块。**若引入 Suspense 有风险就跳过，只做 manualChunks**（本任务优先保零回归）。
3. 复核 gzip 后主块 < 500kB 告警阈值（或至少明显下降）。

## 文件
- 改 `web/vite.config.ts`（build.rollupOptions.output.manualChunks；如做懒加载则 `web/src/App.tsx` + Canvas 挂载处）
- 如懒加载：补/改相应组件测试
- 重建 `internal/serve/dist`（本地嵌入版，`npm run build`）并提交

## 纪律
- **零功能变化 / 零回归**：tsc + 全量 vitest + playwright 必须全绿。
- manualChunks 只影响打包分块,不改运行行为。懒加载若做,Suspense fallback 要有(避免闪白)。
- verify 后确认 `npm run build` 输出的主 chunk 变小（把 build 的 chunk 体积贴进 evidence）。
- 完成跑 verify 绿再 checkpoint。

verify: cd web && npx tsc -b && npx vitest run && npx playwright test
