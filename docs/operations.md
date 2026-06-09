# Pactify — Operations

> Last updated: 2026-06-09 | Status: Placeholder

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

## 发版

```bash
./scripts/release.sh [patch|minor|major]
```

（release.sh 待 CLI 实现后创建）
