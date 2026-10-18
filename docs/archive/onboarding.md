> Archived 2026-10-18 (AJ-744): governance mode was retired; see tag v0.12.0.

# Onboarding — 5 分钟把一个新项目接入 Pactify

把任意 repo 接入 Pactify，让你已装的 agent（opencode / Claude / agy / Codex / Kimi …）
协同开发它。零外部服务——协调层就是你的 git + `.pact/` 文件。

## 前置：装 pactify（一次）

```bash
curl -fsSL https://pactify.dev/install.sh | sh   # 装 pactify 二进制
pactify doctor                                    # 自检安装 + agent 探测
```

## Step 1 — 注册你的 agent（一次，机器级）

让 pactify 知道你这台机器上装了哪些 agent：

```bash
pactify agent scan        # 探测已装 agent
pactify agent register opencode   # 注册
pactify agent register claude-code
```

注册是机器级的（写 `~/.pactify/`），所有项目共享。

## Step 2 — 在新项目里 setup

**A. CLI（任何 repo 内）**

```bash
cd /path/to/your-project
pactify setup            # 引导：建议座席 + 角色，确认后 init + wire
```

`setup` 会 scaffold `.pact/`、给每个座席烤 entry 文件（`AGENTS.md`/`CLAUDE.md`）、把 pact MCP
server 接进 agent 配置。非交互环境用 `pactify setup suggest` 看建议，再手动 `init` + `agent add`。

Dashboard 现用于 harness jobs（Jobs、Dashboard、Settings、Live），不再提供治理模式的
Setup 页面或 setup API（AJ-723）；项目初始化使用上面的 CLI。

## Step 3 — 派发、评审与合并

旧的 `plan`、`orchestrate`、`finish` 等编排命令已在 AJ-735 删除；需要回滚时使用 tag `v0.12.0`。
可选 pact 模式由人或 agent 显式执行任务流，每一步使用对应座席身份：

```bash
# Coordinator
pactify assign T1 --feature F --branch feat/x --owner w --reviewer rev --spec .pact/tasks/T1.md
# Worker w: join, implement, then submit
pactify join w --roles worker --task T1
pactify checkpoint T1 --evidence "tests passed"
# Reviewer rev: inspect and verify, then accept
pactify accept T1 --evidence "review and tests passed"
# Coordinator: merge after all tasks are accepted
pactify merge F
```

harness 委派使用 `pactify sub`，见 [README](../README.md#claude-code-one-click)。

---

## Seat 定义详解

座席（seat）= 一个自声明的稳定身份。`pactify init` 的每个 `--seat` 是
`id:roles:entry[:kind]`：

| 字段 | 含义 | 例 |
|---|---|---|
| `id` | 座席 id（slug：小写字母/数字/`-`） | `opencode-worker` |
| `roles` | 逗号分隔：`orchestrator` / `reviewer` / `worker` | `worker` 或 `orchestrator,reviewer` |
| `entry` | 该座席 agent 的入口文件（相对路径，无 `/` 开头、无 `..`） | `AGENTS.md` |
| `kind` | （可选）agent kind，用于 wire 配置 | `opencode` |

**两条铁律**（协议保证职责分离）：① worker 不能自接受（只有该 task 的 reviewer 能 accept，且需先 checkpoint）；
② feature 的所有 task 都 accepted 才能 merge。所以 `owner ≠ reviewer`。

> 可用 `pactify seat add` 增补座席；需要具有 orchestrator 协议角色的身份。

---

## 部署 dashboard（含公网）

`pactify serve` 提供 `/api/sub` 和静态资源，旧治理 API 已删除。`--seat`、`--project`
是隐藏的空操作兼容参数（AJ-735）。

```bash
pactify serve --addr 127.0.0.1:7777
```

浏览器写请求受 CSRF Origin guard 保护；反向代理域名需配置 `PACTIFY_ALLOWED_ORIGINS`。
公网访问仍需在反向代理（例如 Cloudflare Access）认证本人；Origin guard 不是登录认证。
定时编排已删除；旧版行为见 tag `v0.12.0`。
