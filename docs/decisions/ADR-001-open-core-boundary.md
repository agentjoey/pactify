# ADR-001: 开源/商业化边界 = 守 Team

> 状态：Accepted | 日期：2026-06-09 | 决策者：Joey

## 背景

Pactify 分三个产品层：Pact-Base（协议机制）、Pact-Squad（可视化编排）、Pact-Team（团队云）。
需要决定商业化边界画在哪——哪些免费开源，哪些付费。

## 决策

**守 Team。**

- **Pact-Base**：完全免费开源（协议 + CLI + MCP server + 只读本地 dashboard）
- **Pact-Squad**：可视化编排**主功能免费开源**；**部分高级 feature 付费**（具体后期定义）
- **Pact-Team**：付费商业化（云端 relay + 多人协作 + 托管 + RBAC + 企业功能）

## 理由

1. **护城河在云端，不在本地。** 开源项目里纯本地的付费功能几乎守不住——有人会 fork 掉 feature gate。真正不可复制的是 Team 的云端 relay + 多人协作 + 托管基础设施。
2. **Squad 是病毒式 demo。** 可视化编排是最强的传播素材，主功能免费才能驱动采用。
3. **采用优先于变现。** 早期目标是分发 + 学习；护城河晚在"状态/记忆/体验/社区"，不在早期 feature 数量。

## 工程含义

- 一个 monorepo、三个发布层，靠 feature gate 解锁——**不是三个独立 repo**。
- Squad 的付费 feature gate 要设计成"加法"（不破坏免费主功能闭环），参照 R5 解耦原则。
- 所有付费价值最终落在云端 relay 之上 → log.jsonl 事件 schema（Phase 1 冻结）必须同时服务本地（免费）和云端（付费），零改动 agent 端。

## 备选方案（未采纳）

- **守 Squad**（Squad 整体付费）：多一层变现，但纯本地付费易被 fork，护城河弱。
