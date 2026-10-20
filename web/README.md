# Pactify web

`pactify serve` 与只读 `pactify sub panel` 的共享前端，使用 React、TypeScript 和 Vite。

在 `web/` 运行：`npm install`；`npm run dev`（API 代理默认指向 `127.0.0.1:7777`，可用 `PACTIFY_SERVE_URL` 覆盖）。
验证：`npx tsc --noEmit && npx vitest run`；构建两份产物后运行 `npx playwright test --retries 0`。
构建：`npm run build && npm run build:panel`，分别生成 `internal/serve/dist` 与 `internal/subpanel/dist`；`npm run e2e` 会依次构建两份并运行浏览器测试。
