# Pactify — Operations

> Last updated: 2026-06-13 | Status: Placeholder

## 日常开发流程

```bash
git pull
cat .agent/CURRENT.md     # 查当前状态
cat .agent/sprints/sprint-001.md  # 查当前 Sprint 任务
```

## 故障排查

| 症状 | 排查步骤 |
|------|---------|
| STATE.yml 与 log.jsonl 不一致 | `pactify log` 重算，用输出覆盖 STATE.yml |
| task 状态无法转换 | 检查契约规则（worker 不能自标 accepted） |

## relay 配置与运维

`pactify serve` 可将事件 POST 到远端端点，用于云端 relay / audit。不配置时 relay 完全禁用，零开销。

### 启用 relay

```bash
# flag（优先级最高）
pactify serve --relay-url https://relay.example.com/api/events --relay-token "$(security find-generic-password -s pactify-relay -w)"

# 或仅环境变量
export PACT_RELAY_URL=https://relay.example.com/api/events
export PACT_RELAY_TOKEN=$(security find-generic-password -s pactify-relay -w)
pactify serve
```

token 走 **env / Keychain**，不在命令行或配置文件中写入明文 token。

### 监控

- **dropped 计数**：`GET /api/projects` 响应中无直接暴露 relay dropped；运行时指标在 serve 进程日志中。dropped > 0 表示事件因队列满或端点不可达而丢失。
- **健康**：relay 是异步 best-effort，单点失败不阻塞本地工作流。relay 挂掉时，本地 SSE / CLI / dashboard 不受影响。
- **server 日志**：serve 进程 stdout 无 relay 内部日志（静默丢弃），检查远端端点日志确认送达。

### 禁用 relay

清空 URL 即可（默认状态）：

```bash
unset PACT_RELAY_URL PACT_RELAY_TOKEN
pactify serve   # relay 禁用
```

## 发版

```bash
./scripts/release.sh [patch|minor|major]
```

（release.sh 待 CLI 实现后创建）

## orchestrate 自主驱动（pactify orchestrate）
写好任务图（assign 各 task + owner/reviewer/deps + 每个 task 规格里加一行机器可读 `verify: <命令>`），然后让 orchestrate 跑到底：

```bash
# 在 repo 根（含 .pact/）运行；为每个参与座席指定 headless runner kind
pactify orchestrate \
  --seat-kind w=opencode \
  --seat-kind orch=claude-code
```
- **座席→kind**：`--seat-kind seat=kind`（可重复）。有 headless runner 的 kind：`opencode`/`claude-code`/`gemini-cli`。GUI/桌面 agent（antigravity、*-desktop）无法被驱动——那一棒换 CLI 座席或人工。
- **task 规格 `verify:` 字段**：硬测试门与 reviewer 都用它跑验收，例 `verify: go test ./internal/serve/ -run Relay`。缺失则退化为全量 `go build ./... && go test ./...`。**只放一行专用 `verify:` 指令，勿写成散文**（首条 `verify:` 行胜出；`>`/`-`/`#` 前缀的不计）。
- **flags**：`--feature <id>`（只跑某 feature）、`--dry-run`（只打印下一动作不拉 agent）、`--max-rework`(3)/`--max-fails`(2)/`--max-iters`(50)。
- **卡住升级**：返工/失败超阈值或硬门失败 → orchestrate 暂停，写 `.pact/orchestrate/escalation-<ts>.md`（含 task/原因/evidence/建议）并通知。人工修复（改实现/改规格/修协议）后**重跑同一命令即续行**（状态已前进；`--resume` 是文档性同义）。
- **secrets**：runner 不在命令行传 token；agent 自身凭据由其自身配置/Keychain 管。
