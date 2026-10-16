# Jobs view

在 dashboard 顶部与 Dashboard / Board / Cockpit 并列的 **Jobs** 标签页查看委派任务；手机上从 **View** 下拉框选择 Jobs。视图选择沿用 dashboard 的 `pactify:lens` localStorage 设置，不依赖 `.pact` 项目注册。列表跨仓库展示，可按状态、harness、仓库筛选；running 在前，queued 随后，其余按创建时间倒序。

列表展示标题 / issue、harness、状态、仓库、访问权限与 worktree、token、活动与待处理事项。打开任务可看 brief、结果摘要、自动跟随的可读日志、按文件折叠的 diff 和续接链。两种入口共用 dashboard 的设计 token、UI 组件和 Jobs 样式。dashboard 原本只有固定深色配色（`index.css` 的 `color-scheme: dark`），Settings 的 Appearance 目前是占位页；嵌入的 Jobs 跟随该配色，不显示独立页头或 Theme 选择框，也不读写全局主题。独立 panel 保留自己的 System / Light / Dark 选择，浅色覆盖只在 panel 入口加载。

可直接打开 `https://pactify.agentjoey.ai/#/jobs` 或 `https://pactify.agentjoey.ai/#/jobs/<id>`，加载和 hashchange 都会优先进入 Jobs，并打开对应详情。切换到其他视图时清除 Jobs hash；从标签或手机 View 重新进入 Jobs 时生成 `#/jobs`，详情的 All jobs 链接也返回该地址。

## 通过 pactify.agentjoey.ai 访问

Owner 当前部署的访问路径：

```text
浏览器 → Cloudflare Access 登录 → Cloudflare Tunnel（本机 cloudflared）
       → http://localhost:17082 → launchd 托管的 pactify serve
```

打开 `https://pactify.agentjoey.ai`，完成 Access 登录，然后选择 **Jobs**。前提是 Tunnel 正在运行，且该域名的 Cloudflare Access 保护已启用。serve 自身没有登录认证；`internal/serve/security.go` 的 Origin 校验只保护浏览器写请求，不能替代 Access。

此部署中 **serve 必须只监听本机 loopback**。启动参数应为 `pactify serve --addr 127.0.0.1:17082`，不要绑定 `0.0.0.0` 或局域网地址。CLI 的默认端口仍是 `127.0.0.1:7777`；`--addr` 是现有的可配置参数，本变更不调整它。其他 dashboard 写操作若经反向代理使用，需将完整 origin 加入 `PACTIFY_ALLOWED_ORIGINS`（例如 `https://pactify.agentjoey.ai`）；Jobs 只发 GET，不需要为它单独放宽 Origin。

API 使用 serve 进程的 `PACTIFY_HOME`，与 `sub` 命令同样解析为 `$PACTIFY_HOME/jobs`，未设置时为 `~/.pactify/jobs`。launchd 和 CLI 的环境变量若不同，会看到不同的任务。浏览器不会直接访问文件系统。

## 本机 sub panel

```sh
pactify sub panel
pactify sub panel --port 17100 --no-open
```

默认选择随机 loopback 端口并打开浏览器；`--no-open` 只打印 URL。只能监听 `127.0.0.1`，Ctrl-C 退出。首次打开 `http://127.0.0.1:<port>/?token=…` 会消耗一次性 launch token，并交换为独立 session token；再次使用原始 launch URL 返回 401。

前端移除 URL 中的 token，将 session 保留在当前标签页的 `sessionStorage`，刷新仍可用。API 与 fetch 日志流使用 `Authorization: Bearer <session>`；不使用认证 cookie。重启 panel 后需使用新 URL。panel 的认证与 serve 的宿主安全模型互相独立。

## API 与边界

两种宿主挂载同一个 `subpanel.ReadOnlyHandler`，不复制处理逻辑：

| 功能 | dashboard | 本机 panel |
| --- | --- | --- |
| 列表 | `/api/sub/jobs?all=1` | `/api/jobs?all=1` |
| 详情 | `/api/sub/jobs/<id>` | `/api/jobs/<id>` |
| SSE 日志 | `/api/sub/jobs/<id>/logs` | `/api/jobs/<id>/logs` |
| diff | `/api/sub/jobs/<id>/diff` | `/api/jobs/<id>/diff` |

全部 API 仅接受 GET，HEAD 也拒绝。不提供 apply、discard、cancel、resume 操作；请在 CLI 或 Claude Code 中执行。读取使用原子 state 快照，不恢复孤儿 worker、不创建锁文件、不改变任务状态。缺失 `state.json` 的残缺 job 目录会跳过；非法 job id 经 Store 校验拒绝。

日志从末尾 1 MiB 窗口内最多 100 条可读记录开始，半行缓冲最多 64 KiB，浏览器最多保留 1,000 行；任务进入终态后关闭流，离开详情时取消请求。断流 / 错误会显示状态，刷新可重新连接。完整日志使用 `pactify sub logs <id> --follow`。

worktree diff 相对记录的 base commit，包含未跟踪文件，不 staging、不修改 Git index。上限为 512 KiB / 10 秒；截断会显示提示。无 worktree、已 discard 或无法读取时展示记录的变更清单 / 提示，当前结果格式不保存完整 patch。结果文件的既有读取上限为 4 MiB。

## 构建与验证

```sh
cd web
npx tsc --noEmit
npx vitest run
npm run build
npm run build:panel
npx playwright test e2e/jobs.spec.ts e2e/panel.spec.ts
node scripts/shots-panel.mjs
```

仅重建 dist 不会更新正在运行的 launchd 服务。宿主机完成验收后，还需在仓库根目录重新编译带 embed 资源的二进制（`go build -o pactify ./cmd/pactify`），在 macOS 上执行 `codesign --force -s - ./pactify`，再按现有部署配置替换并重启 serve；见 [部署](deployment.md) 与 [运维](operations.md)。

两个构建分别生成受 Git 跟踪的 `internal/serve/dist` 与 `internal/subpanel/dist`，均需随源码更新。CI 会重建并检查 drift。Jobs e2e 使用 mock-server 直接服务最终 serve dist，并在测试产物中附上桌面 / 手机列表与详情截图；不同系统配色偏好下，dashboard 都保持原有深色，另有 390px 深链接用例；panel 截图脚本用独立 mock 端口服务最终 panel dist，不依赖常驻 daemon。宿主机还需检查截图效果与外部 Tunnel + Access 的实际访问。
