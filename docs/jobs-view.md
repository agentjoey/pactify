# Jobs view

The dashboard (`pactify serve`) and GET-only panel (`pactify sub panel`) share **Dashboard / Jobs / Settings** navigation. Jobs is the default page. The header shows attention counts, a search trigger (⌘K or `/`), live polling age, Settings and a theme control. On phones, navigation stays in a segmented row and content has a 16px gutter. Live remains on Dashboard and in job details.

Both builds default to **dark**, independently of the OS. Choose **light** or **dark** from the header or **Settings → Appearance**. The choice is saved in this browser's localStorage when available. Space Grotesk and JetBrains Mono are served locally; no Google Fonts requests or CSP changes are needed. Motion respects `prefers-reduced-motion`.

## Pipeline and list

**Lines** is the default Jobs view. Its four stations use the existing latest-run section rule:

| Station | Jobs |
| --- | --- |
| 01 Queued | `queued`, even with attention |
| 02 Running | `running` or `retrying`, even with attention |
| 03 Needs you | Other statuses with attention |
| 04 Shipped | Other statuses without attention |

Group production lines by **Project** (repository basename), **Harness**, or **One line**. Each line shows observed harnesses or project counts, its running share and jobs finished today. Belts move only on lines with running jobs. Statistics use actual jobs, timestamps and available actions; there is no inferred capacity or execution stage. Shipped shows the three most recent jobs per line, with **+N more** expanding that line in place. Completion-day totals use the browser's local calendar.

On desktop, the line label rail stays fixed while stations scroll horizontally. Scroll padding reserves its 220px width so Queued remains visible. Below 640px, stations become a full-width horizontal pager, initially on **Running**. Tap a station tile or swipe; highlighted tiles and page dots track the visible station. The board handles empty/filter-empty states, while onboarding, loading, server errors and expired sessions retain explicit guidance.

Switch to **List** for the existing sectioned cards, completion-day stacks and optional **Compact list** with eight aligned metadata columns. The Lines/List choice is saved in localStorage when available. Project, Harness, Status and search filters remain in the URL and work in both views. In Lines, the view switch, grouping, search and shortcuts share one toolbar; **Filters** reveals the other controls. Phone filters also use this disclosure in List. Search stays visible and `/` focuses it in both views. Search matches title, ID, repository and requested or actual model. Explicit resume/fallback links form chains; matching any run retains the whole chain. Chains use their root title and latest run's status, model, duration and usage. Manual resume links count as rework; automatic retries do not.

Use `/` or **⌘K** to search, **j/k** for next/previous job, **h/l** for previous/next station in Lines, **Enter** for details and **Escape** to close. **?** opens shortcut help. Typing inside inputs does not trigger job navigation.

Open `#/jobs` or `#/jobs/<id>` directly. The drawer preserves filters and shows brief, result, Live, bounded readable logs, file diffs and run timelines. Dashboard actions retain their existing confirmations for cancel, resume, apply and discard. Needs-you cards in the panel instead offer copyable terminal commands. The panel sends GET requests only and keeps its strict CSP and session handling.

## Dashboard and Settings

Dashboard starts with five KPIs: in progress, queued, attention, succeeded in seven local calendar days and failed in seven local calendar days. Phones use a 2+3 tile layout. The main column contains running and attention jobs, the Live tree, a 14-day token chart and harness usage. The side column shows installed harness snapshots, quota and recent job activity. Cost, subscription, unknown and partial usage remain distinct; unavailable data is not fabricated.

Settings uses a left navigation and content area, with a section select on phones. **Harnesses / Models & routing** retains installed versions, models, default model/effort, aliases, fallback ordering, quota groups and exhaustion records. Dashboard edits still handle 422 field errors, 409 conflicts and explicit reloads; panel machine settings remain read-only. **Storage** shows per-location sizes and reclaimable estimates, cache usage against its actual cap, policy chips, protected-worktree notices and the reviewed dashboard cleanup flow; the panel provides CLI commands. **Doctor** shows only the existing stored snapshot, never running checks. **Appearance** stores a manual theme and **Shortcuts** documents navigation.

Live displays observed harnesses, models, effort, calls, diff, quota, resume/fallback and retry links. Redacted events are batched per animation frame, with at most four live streams; hidden or unmounted views close their streams. No unobserved agents, stages or quota reset times are inferred.

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
requested and actual models. In List view, compact completed history uses eight aligned desktop
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
