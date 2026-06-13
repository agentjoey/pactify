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
