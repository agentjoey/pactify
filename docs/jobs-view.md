# Jobs view

Web v2 的主导航是 **Dashboard / Jobs / Settings**，窄屏使用 **Page** 下拉框。默认进入 Jobs，路由与筛选由 hash 决定。**Live** 位于 Dashboard 顶部和任务详情的 Live 页签中，不是独立主导航。`pactify serve` 与只读 `pactify sub panel` 共享页面；都使用固定深色配色。

Jobs 跨仓库展示委派任务，支持 Project、Harness、Status 与标题搜索；筛选写入 URL query。列表先按显式 resume/fallback 链合并，再保留任一轮匹配仓库、harness 或标题的整链，不按相同标题或 session id 猜测关系。链首提供标题，最新一轮提供状态、模型、耗时与 token；卡片还显示成本、worktree、retry、fallback 与 failure class。`rework` 只计手动 resume 链接，不含自动 retries。链时间线按因果排序，分叉、环和缺失链接按现有确定性规则处理。

任务分为 **In progress / Queued / Needs attention / Completed**：running/retrying 优先进入 In progress，queued 单列；终态有 attention 进入 Needs attention，其余终态进入 Completed。Completed 按本地完成日期分组，默认折叠为卡片堆叠，可 Show all。列表每 2 秒刷新；窄屏卡片堆叠，详情抽屉全屏。

直接打开 `#/jobs` 或 `#/jobs/<id>`；抽屉按所选轮次显示 brief、结果、Live、可读日志、按文件折叠的 diff 和续接链，关闭返回列表并保留筛选。serve 中可按 summary 的 `actions` 执行 cancel、resume、apply、discard；panel 不显示写操作。整条续接链没有 queued/running/retrying job 时，仅最新已结束且有 session 的 turn 可 resume。

**Dashboard** 展示 Live agent tree、任务指标、最近 7 个本地日历日的 token 趋势、按 harness 的 jobs / success rate / duration / tokens / cost，以及 quota。用量与额度来自独立 cost API，每 30 秒刷新，不随 Jobs 筛选变化。美元、subscription、unknown 与 partial 分开标示；失败显示不可用状态。没有已知 resetAt 时不推断额度重置时间。

Live 使用记录中的 harness、模型、effort、调用、diff、quota、resume/fallback 与 retry 关系，不推测未观察到的 agent。结构化 events 经脱敏后显示，原始载荷不会传入树；无时间戳的记录不显示时间。事件按 animation frame 批处理，同页最多四条事件流，其余依赖摘要刷新；页面隐藏或卸载时关闭流。

**Settings** 展示四个 harness 的安装状态、版本、实验标记、read-only 能力、模型列表和默认 model/effort；serve 可保存默认值、刷新模型。Routing 位于 Settings 内，展示配置路径、别名、有序回退链、额度组与耗尽记录，支持 Edit / Save / Cancel、回退排序及 Clear 的页内确认。panel 的 Settings / Routing 只读。422 在字段旁显示，409 提示配置已在别处更改，Reload 丢弃草稿。Routing 每 30 秒刷新，编辑期间保留草稿及原版本。接口及锁、版本契约见 [路由文档](router.md#jobs-页管理路由aj-700)。

## 通过 pactify.agentjoey.ai 访问

Owner 部署的访问路径：

```text
浏览器 → Cloudflare Access 登录 → Cloudflare Tunnel（本机 cloudflared）
       → http://localhost:17082 → launchd 托管的 pactify serve
```

打开 `https://pactify.agentjoey.ai/#/jobs`，完成 Access 登录。前提是 Tunnel 与该域名的 Access 保护已启用。serve 自身没有登录认证，Origin 校验仅保护浏览器写请求，不能替代 Access。

此部署中 serve 必须只监听 loopback：`pactify serve --addr 127.0.0.1:17082`。CLI 默认仍为 `127.0.0.1:7777`。经反向代理编辑 Settings、Routing 或执行 Jobs 操作前，将完整 origin 加入 `PACTIFY_ALLOWED_ORIGINS`，例如 `https://pactify.agentjoey.ai`。

API 使用 serve 进程的 `PACTIFY_HOME`，与 CLI 一样读取 `$PACTIFY_HOME/jobs`，未设置时为 `~/.pactify/jobs`。launchd 与交互式 shell 的环境不同，会影响任务目录和 resume 所需配置；shell 中导出的变量不会自动进入服务。worker 不继承 `PACTIFY_ALLOWED_ORIGINS` 和 `PACTIFY_LEDGER_REF`。服务在后台回收已结束的 worker。

## 本机 sub panel

```sh
pactify sub panel
pactify sub panel --port 17100 --no-open
```

默认选择随机 loopback 端口并打开浏览器；`--no-open` 只打印 URL，只能监听 `127.0.0.1`，Ctrl-C 退出。首次访问 `http://127.0.0.1:<port>/?token=…` 消耗一次性 launch token，换取独立 session token；重用原 URL 返回 401。

前端移除 URL 中的 token，将 session 保留在当前标签页的 `sessionStorage`。API 与 fetch 日志流使用 `Authorization: Bearer <session>`，不使用认证 cookie；重启 panel 后需使用新 URL。panel 认证与 serve 的宿主安全模型独立。

## API 与边界

两种宿主挂载同一个 `subpanel.ReadOnlyHandler`：

| 功能 | serve | 本机 panel |
| --- | --- | --- |
| 列表 / 详情 | `/api/sub/jobs?all=1`、`/api/sub/jobs/<id>` | `/api/jobs?all=1`、`/api/jobs/<id>` |
| SSE 日志 / 事件 | `/api/sub/jobs/<id>/{logs,events}` | `/api/jobs/<id>/{logs,events}` |
| diff | `/api/sub/jobs/<id>/diff` | `/api/jobs/<id>/diff` |
| harness 状态 / 模型 | `/api/sub/harnesses`（`?refresh=1` 刷新） | `/api/harnesses` |
| 路由配置 | `GET /api/sub/router` | `GET /api/router` |
| 用量汇总 | `/api/sub/cost?since=7d&by=harness` | `/api/cost?since=7d&by=harness` |
| 保存路由 / 默认值 | `PUT /api/sub/router` | 不注册 |
| 清除耗尽记录 | `DELETE /api/sub/quota/<encoded-key>` | 不注册 |
| 任务操作 | `POST /api/sub/jobs/<id>/{cancel,resume,apply,discard}` | 不注册 |

共享只读 API 仅接受 GET，HEAD 也拒绝。serve 的写操作经过 Origin guard，仅接受 JSON，请求体上限 64 KiB，状态冲突返回 409。Dashboard resume 每 store 最多四个活跃 job（含 queued/retrying），超限返回 429；CLI 不占此额度。结束、取消或启动失败释放名额。CLI 与 dashboard 共用 session lock。

任务读取使用原子 state 快照和单次索引，不恢复孤儿 worker、不创建锁文件、不修改任务状态。缺失 `state.json` 的目录会跳过，非法 job id 被拒绝。cost API 的参数及错误沿用 [`sub cost`](sub-cost.md)，额外返回过滤过期记录后的 `exhaustedRoutes`，不根据历史 `quotaSnapshots` 推断耗尽或重置时间。

日志从末尾 1 MiB 窗口内最多 100 条可读记录开始，半行缓冲最多 64 KiB，浏览器最多保留 1,000 行；终态关闭流，离开详情取消请求，断流 / 错误显示状态。完整日志使用 `pactify sub logs <id> --follow`。

worktree diff 相对记录的 base commit，包含未跟踪文件，不 staging、不修改 Git index；上限为 512 KiB / 10 秒，截断显示提示。无 worktree、已 discard 或无法读取时展示已有变更清单 / 提示，result 不保存完整 patch；结果文件读取上限为 4 MiB。

## 构建与验证

```sh
cd web
npx tsc --noEmit
npx vitest run
npm run build
npm run build:panel
npx playwright test --retries 0
node scripts/shots.mjs
node scripts/shots-live.mjs
node scripts/shots-panel.mjs
```

`build` 与 `build:panel` 分别生成 Git 跟踪的 `internal/serve/dist` 与 `internal/subpanel/dist`；`npm run e2e` 依次构建两份再运行 Playwright。测试与截图脚本使用独立 mock 服务最终 embedded dist，不依赖常驻 daemon。

重建 dist 不会更新 launchd 服务。host 完成验收后需重新编译 embed 二进制（`go build -o pactify ./cmd/pactify`）、在 macOS 上 `codesign --force -s - ./pactify`，再按现有配置替换并重启 serve；见 [部署](deployment.md) 与 [运维](operations.md)。host 还需检查截图和外部 Tunnel + Access 的实际访问。
