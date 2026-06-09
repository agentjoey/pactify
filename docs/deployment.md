# Pactify — Deployment

> Last updated: 2026-06-09 | Status: Placeholder（CLI 实现后补充）

## 安装（规划）

```bash
# Go（单静态二进制）
brew install pactify    # 或
go install github.com/agentjoey/pactify@latest

# Node
npm install -g pactify
```

## 在项目中使用

```bash
cd your-repo
pactify init          # 生成 .pact/ 骨架
# 编辑 .pact/PROJECT.md 填写章程
# 用 git 管理 .pact/
git add .pact/ && git commit -m "chore: init pactify"
```

## 开发环境（本 repo）

CLI 语言待 Sprint 001 T1 决策后补充。
