# 蚂蚁动效语言 —— Pactify 的 signature 交互体系（2026-06-14）

> 用户拍板（D4）：**蚂蚁的动效就是 Pactify 有辨识度的交互体系**。不是装饰，而是**用蚂蚁的行为直接表达 agent 之间的通信与互动状态**——你不靠读状态文字，而是**看蚂蚁在干什么**就懂系统在发生什么。
> 配合 D1（浅色）、D2（不是为了 3D，而是用这种布局/呈现方式动态表现 agent 关系与互动态）。

## 核心理念
agent / 座席 = **节点**；关系（owner/reviewer/dep）= **边**；**蚂蚁 = 边和节点上流动的"活的状态"**。系统的实时态由蚂蚁的 **速度 / 方向 / 形态 / 颜色 / 数量** 编码，一眼可读。

## 动效语法（grammar）—— 五个维度编码语义
| 维度 | 编码 |
|------|------|
| **速度** | 快速一次性 = 离散事件（下发/交接）；匀速持续 = 进行中（干活/通讯） |
| **方向** | orch→worker = 下发；worker→reviewer = 交接送审；reviewer→worker = 打回返工 |
| **形态** | 直线爬 = 传输/通讯；**原地转圈** = 等待/挂起；**汇聚** = 合并；**钻出** = spawn |
| **颜色** | role 色 = 正常；琥珀 = 返工；**红 = 报错/卡住** |
| **数量/密度** | 蚂蚁越多 = 活动越密（性能上限封顶） |

## 状态/事件 → 蚂蚁行为 词表（the vocabulary）
| pact 状态/事件 | 蚂蚁行为 | 一眼读出 |
|---|---|---|
| **任务下发**（orchestrator 派发 / launch worker） | 1–2 只**载物蚂蚁**（带货）**快速**一次性从 orchestrator 爬到 worker | "下发任务了" |
| **worker 干活中**（in_progress / 持续通讯） | **匀速持续**的蚂蚁流在 worker 的 lane（或 worker↔orch）上循环爬 | "在持续干活/通讯" |
| **等待 review**（awaiting_review） | **信使蚂蚁在本 agent 节点原地转圈**（holding） | "干完了，在等 review" |
| **reviewer 评审中** | 信使蚂蚁在 task↔reviewer 之间**往返**穿梭 | "reviewer 在审" |
| **打回返工**（changes_requested） | 一只蚂蚁**反向**（reviewer→worker）带东西回去，**琥珀色** | "被打回，返工" |
| **通过**（accepted） | 蚂蚁抵达 reviewer + 一记**绿色"已交付"脉冲**，lane 归于平静 | "通过了" |
| **spawn 新 session** | 一只**新蚂蚁从该 agent 的工位钻出**（spawn-in） | "起了个新 session" |
| **报错 / 升级 / 卡住** | 蚂蚁转**红** + 动作**焦躁/抖动**或在阻塞节点**堆积** | "报错/卡住，要你介入" |
| **合并 / 交付**（merge / shipped） | 各已验收 task 的蚂蚁**汇聚**到 feature 节点 → ship 脉冲 → 平静 | "合并交付了" |
| **空闲**（idle） | 无蚂蚁，或一只**慢速巡逻**蚂蚁 | "空闲" |

## 蚂蚁的"角色"（caste，已有基础可复用）
- **载物蚁（carrier）**：带货块——用于"下发/交接"有产物的传输（任务、checkpoint evidence）。
- **信使蚁（messenger）**：无货——用于"评审往返/等待转圈"等纯通讯。
- 颜色随**该条关系的语义/健康**：正常取两端 role 色的渐变；返工琥珀；报错红。

## 与现有实现的关系
- 现有 `AntEdge`（`animateMotion` + `<mpath>` 沿边爬 + carrier/messenger 两种 caste + reduced-motion 抑制）是**正确地基**——但当前只在画布、只表达 dep/wait 两态。
- 升级方向：把上面这张**词表**接到 **orchestrate 实时态 + pact 状态**上，让蚂蚁在 Office/画布/Live 里真正"演"出系统在发生什么；速度/方向/转圈/汇聚/钻出/红色焦躁这些**新形态**要补。
- **reduced-motion**：动效全程可降级（静态 = 用状态 pill + 颜色表达，不丢信息）。

## 与浅色（D1）的配合
- 浅色底上蚂蚁要**够对比**：深色蚂蚁剪影 + role 色描边/拖尾；lane 用浅灰，活跃 lane 才上色。
- 蚂蚁的"拖尾/微光"在浅色下用低透明 role 色，避免脏。

## 落地次序（配合 D3）
蚂蚁语言是贯穿件，分阶段接入：
1. 先做 **Agent Config（dify TOOLS 模式）** + **浅色 token 基线** + **状态 pill 系统**（②⑥，蚂蚁先不动）。
2. 再做 **Comms/Links 面板**（make grid 式方向 pill）——静态关系先立住。
3. 然后把**蚂蚁词表**接到 Live/Office：先 3–4 个高频态（下发 / 干活 / 等待转圈 / 报错红），再补 spawn/汇聚等。
4. proximity-connect 编排 + edge-routing 在画布层并入。
