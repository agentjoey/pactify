# Task t5 · ui

**Feature:** agents · **Owner:** opencode-worker · **Reviewer:** claude · **Deps:** t4

## 目标
dashboard Agents 面板——空注册表即 onboarding 第一屏，非空即管理器。机器级（非项目内）。

## 改文件
新建 `web/src/components/Agents.tsx` + `Agents.test.tsx`；`web/src/lib/api.ts` 加 getAgents()/registerAgent(kind,label?)/unregisterAgent(kind)；接入顶层（参照现有顶层面板挂法，非项目内）。

## 行为
- getAgents() → GET /api/agents，类型 `AgentRow{kind, installed, detail, registered, label}`。
- 无 registered → onboarding 态：标题"扫描到这些 agent，选择注册以开始" + installed agents 列表。
- 非空 → 管理器：每行 `kind · installed 徽章（已装/未检测到）· [注册/已注册 开关]`，未 installed 灰显。
- 开关 = registerAgent/unregisterAgent + 乐观刷新。复用 ui/ 基础件（Button/Badge）。observe（author=false）开关禁用。

## 验收
Agents.test.tsx（vitest + mock fetch）：空 registry 渲染 onboarding 文案 + installed 行；点注册调 registerAgent 乐观显示已注册；点已注册调 unregisterAgent；未 installed 灰显且禁用；observe 禁用。

## verify
```
cd web && npx vitest run src/components/Agents
```

## 完成方式
TDD。座席 opencode-worker。**web/src 改动同 commit 重建 dist：`cd web && npm run build`**。checkpoint t5 附 verify 输出。不自标 accepted。
