# Pactify Backlog

产品候选功能（未排期）。源头标注。

## 来自竞品分析（AgentHub，2026-06-13）
- **动作模板 / orchestrate 配方**：把常见多 agent 工作流做成一键配方（"加测试""共同评审""从需求到设计计划"），架在 orchestrate 之上——把门槛从"手写任务图 + assign"降到"选配方"。竞品 AgentHub 已有同款。
- **自然语言命令面板**：dashboard 里一个"输入指令 → 驱动 agent"的对话入口 + "等待你的"人在环状态，作为人→编排者的交互面（当前 orchestrate 仅 CLI 驱动）。

## 差异化备忘（对 AgentHub）
- AgentHub 靠中心化 "Hub"（服务器）协调，更新日志在反复修"Hub 连接不稳定"。Pactify 用 git+文件当唯一事实源、零服务器——**主打"没有 Hub 可以掉线、你的 repo 就是协调层"**，是 ADR-001"守 Team"之外更底层的技术护城河。
