# Jobs view

Web v2 uses **Dashboard / Jobs / Settings** navigation, with a **Page** selector on narrow screens. Jobs is the default page; hash routes preserve navigation and filters. **Live** appears on Dashboard and in job details. The dashboard and read-only panel share these pages; the dashboard uses a dark palette and the panel follows the system theme.

Jobs displays work across repositories and supports Project, Harness, Status and text filters in the URL. Search matches title, job ID, repository and requested or actual model. Explicit resume/fallback links form chains; matching any run retains the whole chain. Chains use the root title and latest run status, model, duration and usage. Cards also show costs, worktrees, retries, fallbacks and failure classes. Rework counts manual resume links, not automatic retries. Timelines use causal order and handle branches, cycles and missing links deterministically.

任务分为 **In progress / Queued / Needs attention / Completed**：running/retrying 优先进入 In progress，queued 单列；终态有 attention 进入 Needs attention，其余终态进入 Completed。Completed 按本地完成日期分组，默认折叠为卡片堆叠，可 Show all。列表每 2 秒刷新；窄屏卡片堆叠，详情抽屉全屏。

直接打开 `#/jobs` 或 `#/jobs/<id>`；抽屉按所选轮次显示 brief、结果、Live、可读日志、按文件折叠的 diff 和续接链，关闭返回列表并保留筛选。serve 中可按 summary 的 `actions` 执行 cancel、resume、apply、discard；panel 不显示写操作。整条续接链没有 queued/running/retrying job 时，仅最新已结束且有 session 的 turn 可 resume。

**Dashboard** 展示 Live agent tree、任务指标、最近 7 个本地日历日的 token 趋势、按 harness 的 jobs / success rate / duration / tokens / cost，以及 quota。用量与额度来自独立 cost API，每 30 秒刷新，不随 Jobs 筛选变化。美元、subscription、unknown 与 partial 分开标示；失败显示不可用状态。没有已知 resetAt 时不推断额度重置时间。

Live 使用记录中的 harness、模型、effort、调用、diff、quota、resume/fallback 与 retry 关系，不推测未观察到的 agent。结构化 events 经脱敏后显示，原始载荷不会传入树；无时间戳的记录不显示时间。事件按 animation frame 批处理，同页最多四条事件流，其余依赖摘要刷新；页面隐藏或卸载时关闭流。

**Settings** 展示四个 harness 的安装状态、版本、实验标记 (agy only; both opencode majors are supported)、read-only 能力、模型列表和默认 model/effort；serve 可保存默认值、刷新模型。Routing 位于 Settings 内，展示配置路径、别名、有序回退链、额度组与耗尽记录，支持 Edit / Save / Cancel、回退排序及 Clear 的页内确认。panel 的 Settings / Routing 只读。422 在字段旁显示，409 提示配置已在别处更改，Reload 丢弃草稿。Routing 每 30 秒刷新，编辑期间保留草稿及原版本。接口及锁、版本契约见 [路由文档](router.md#jobs-页管理路由aj-700)。

## 通过认证反向代理访问

访问路径：

```text
浏览器 → 执行用户认证的反向代理 → http://127.0.0.1:7777 → pactify serve
```

例如打开 `https://dash.example.com/#/jobs`，先完成代理认证。`PACTIFY_ALLOWED_ORIGINS` 用于执行用户认证的反向代理，应配置完整 origin，例如 `https://dash.example.com`；支持逗号分隔和去除末尾斜杠。

默认 `pactify serve --addr 127.0.0.1:7777` 无需 token。所有请求（含静态资源和 SSE）校验 Host：loopback 监听仅接受 `127.0.0.1`、`localhost`、`[::1]` 加监听端口，以及 allowlist origin 的 Host（带或不带其显式端口）。浏览器写请求必须精确匹配该端口的 `http` loopback origin，或列入 allowlist；其他本机端口、`null` 均返回 403。无 Origin 的 CLI / 脚本请求继续允许。这些校验不能代替用户认证。

非 loopback 监听（包括 `0.0.0.0`、`::` 和省略主机）必须配置至少 32 字符的 `PACTIFY_SERVE_TOKEN`，Host 仅接受绑定 IP / 主机名加端口或 allowlist 中的代理 Host。任何地址只要设置 token，所有 `/api/` 请求（含读取和 SSE）都须携带 `Authorization: Bearer <token>`，否则返回 401。静态资源仍公开，但受 Host 校验。Dashboard UI 自身不发送该 token；token 模式用于认证后注入 header 的反向代理，代理也须覆盖 SSE 请求。

API 使用 serve 进程的 `PACTIFY_HOME`，与 CLI 一样读取 `$PACTIFY_HOME/jobs`，未设置时为 `~/.pactify/jobs`。后台服务与交互式 shell 的环境不同，会影响任务目录和 resume 所需配置；shell 中导出的变量不会自动进入服务。worker 不继承 `PACTIFY_ALLOWED_ORIGINS` 或 `PACTIFY_SERVE_TOKEN`。服务在后台回收已结束的 worker。

## 本机 sub panel

```sh
pactify sub panel
pactify sub panel --port 17100 --no-open
```

默认选择随机 loopback 端口并打开浏览器；`--no-open` 只打印 URL，只能监听 `127.0.0.1`，Ctrl-C 退出。首次访问 `http://127.0.0.1:<port>/?token=…` 消耗一次性 launch token，换取独立 session token；重用原 URL 返回 401。

前端移除 URL 中的 token，将 session 保留在当前标签页的 `sessionStorage`。API 与 fetch 日志流使用 `Authorization: Bearer <session>`，不使用认证 cookie；重启 panel 后需使用新 URL。panel 认证与 serve 的宿主安全模型独立。

A new tab opened at the cleaned panel URL can request the existing session
through a BroadcastChannel with a request nonce. Tabs of the same panel origin
share the session through this channel; BroadcastChannel is same-origin only,
and the panel CSP forbids third-party scripts. A tab that already has a session
restores it immediately without waiting for a handoff. It keeps the received
session in its own sessionStorage and uses bearer headers; the session is never
added to a URL or copied command. Without an answering tab, the panel shows
“Session ended” after two seconds. The original launch URL remains single-use.

Polling pauses while the document is hidden, doubles its delay after errors from
2 seconds up to 30 seconds, resets on success, and stops on 401. The header shows
relative update age, documentation and the CLI version; the tab title counts
chains needing attention. Search includes title, job ID, repository and both
requested and actual models. Completed history uses eight aligned desktop
columns: status, title, harness/model, project, duration, tokens, cost and time.
Titles truncate with their full text in a tooltip. Narrow rows put title/status
above one metadata line. Job details show one command list: in the pinned footer
on desktop, or revealed from the compact footer button on narrow screens. Cards and
Usage show reported costUSD, subscription for codex/kimi/agy without a reported
cost, and unknown for other unpriced harnesses.

## API 与边界

两种宿主挂载同一个 `subpanel.ReadOnlyHandler`：

| 功能 | serve | 本机 panel |
| --- | --- | --- |
| 列表 / 详情 | `/api/sub/jobs?all=1`、`/api/sub/jobs/<id>` | `/api/jobs?all=1`、`/api/jobs/<id>` |
| SSE 日志 / 事件 | `/api/sub/jobs/<id>/{logs,events}` | `/api/jobs/<id>/{logs,events}` |
| diff | `/api/sub/jobs/<id>/diff` | `/api/jobs/<id>/diff` |
| Doctor snapshot | `/api/sub/doctor` | `/api/doctor` |
| CLI version | `/api/sub/info` | `/api/info` |
| harness 状态 / 模型 | `/api/sub/harnesses`（`?refresh=1` 刷新） | `/api/harnesses` |
| 路由配置 | `GET /api/sub/router` | `GET /api/router` |
| 用量汇总 | `/api/sub/cost?since=7d&by=harness` | `/api/cost?since=7d&by=harness` |
| 保存路由 / 默认值 | `PUT /api/sub/router` | 不注册 |
| 清除耗尽记录 | `DELETE /api/sub/quota/<encoded-key>` | 不注册 |
| 任务操作 | `POST /api/sub/jobs/<id>/{cancel,resume,apply,discard}` | 不注册 |

共享只读 API 仅接受 GET，HEAD 也拒绝。serve 的所有请求经过 Host 校验，token 模式还要求 API Bearer header；写操作经过上述精确 Origin 校验，仅接受 JSON，请求体上限 64 KiB，状态冲突返回 409。Dashboard resume 每 store 最多四个活跃 job（含 queued/retrying），超限返回 429；CLI 不占此额度。结束、取消或启动失败释放名额。CLI 与 dashboard 共用 session lock。

任务读取使用原子 state 快照和单次索引，不恢复孤儿 worker、不创建锁文件、不修改任务状态。缺失 `state.json` 的目录会跳过，非法 job id 被拒绝。cost API 的参数及错误沿用 [`sub cost`](sub-cost.md)，额外返回过滤过期记录后的 `exhaustedRoutes`，不根据历史 `quotaSnapshots` 推断耗尽或重置时间。

日志从末尾 1 MiB 窗口内最多 100 条可读记录开始，半行缓冲最多 64 KiB，浏览器最多保留 1,000 行；终态关闭流，离开详情取消请求，断流 / 错误显示状态。完整日志使用 `pactify sub logs <id> --follow`。

worktree diff 相对记录的 base commit，包含未跟踪文件，不 staging、不修改 Git index；上限为 512 KiB / 10 秒，截断显示提示。无 worktree、已 discard 或无法读取时展示已有变更清单 / 提示，result 不保存完整 patch；结果文件读取上限为 4 MiB。

The diff response includes `maxBytes` (524288), `truncated`, `patch`, `files` and
`note`. The viewer colours text nodes, shows per-file line counts, and initially
collapses files larger than 400 lines. File headers show a single path for
unchanged names and old → new for renames, with added/removed line counts. A truncated patch shows its size limit and
a copyable `git -C <worktree> diff <base> --` command for the full tracked diff.

`GET /doctor` returns `{lastRun: null, harnesses: [...]}` because doctor persists
verified versions, not complete reports. Each harness has `name`, `installed`,
`version` and `verifiedVersion`. Installed versions come only from stored catalog
entries matching current executable metadata; an absent or changed snapshot
shows “version unknown”. The endpoint never runs a harness, discovers models,
executes doctor, recovers jobs, or writes storage. Settings offers a copyable
`pactify sub doctor` command. `GET /info` returns `{version: "..."}` using the
running CLI's release version. Both routes reject non-GET methods, including HEAD,
and inherit their host's authentication and Host checks.

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

After rebuilding dist, rebuild the Go binary and restart the local server. For macOS background services, see [operations](operations.md). Check screenshots and the actual authenticated proxy path when applicable.
