# Harness 上游更新：检查与响应流程

> 状态：Owner 2026-10-17 要求制定（AJ-719）。适用于 pactify 接入的全部 harness：codex、kimi、opencode、agy，以及以后接入的 Muse。
> 起因：opencode 2.0 改用新的 npm 包 `@opencode/cli` 发布，而原有检查只看 `opencode-ai@latest`，所以没有发现。影响评估见 [`harness-v2/opencode-2-impact.md`](harness-v2/opencode-2-impact.md)。

## 1. 原则

- **上游更新本身不是故障，pactify 静默地出错才是。** 遇到不支持的版本，pactify 必须在启动 harness 之前失败，并说明原因和回退方法，不能等超时或返回含糊的错误。
- **只靠一个包名的 `latest` 不够。** 每个 harness 要列出官方安装方式能装到的所有渠道，包括后继包（kimi 曾从 `kimi-cli` 换到 `kimi-code`，opencode 2.x 从 `opencode-ai` 换到 `@opencode/cli`）。
- **检查从便宜的做起。** 先做离线检查和不登录、不推理的检查，最后才做计费的端到端检查。
- **不替 Owner 升级本机的 CLI。** 升级或降级本机 harness 前要先确认；测试只在隔离的 `HOME` 里安装。

## 2. 渠道与基线

| harness | 官方渠道（`releases.py` 都会查） | 契约基线 | 每周检查（UTC 周一） |
|---|---|---|---|
| codex | npm `@openai/codex` | `internal/cockpit/codexschema/CODEX_VERSION` | `codex-schema.yml`：在 latest 上核对 schema 字段 |
| kimi | npm `@moonshot-ai/kimi-code` | `internal/cockpit/harnessdrift/versions.json` | `harness-drift.yml`：latest 的参数和 ACP 握手 |
| opencode | npm `opencode-ai`（1.x）、`@opencode/cli`（2.x） | `versions.json` 加 `opencode-api.yml` 的固定版本 `1.19` | `opencode-api.yml`：1.19 与 latest 的 v1 OpenAPI |
| agy | 安装器 manifest（`install.sh` 从这里取版本） | `versions.json` | `harness-drift.yml`：latest 的参数和事件名 |

本机的状态另外记在 `~/.pactify/verified-versions.json`，由 `pactify sub doctor` 在全部检查通过后写入。job 遇到下面两种情况会在结果里给出 warning：

- 版本与上次 job 不同：`CLI version changed`；
- 版本没有通过 doctor 检查：`has not been checked`。

## 3. 监测

1. **每周自动检查。** `harness-drift.yml` 的 `releases` job 运行 `internal/cockpit/harnessdrift/releases.py`，读取各渠道的公开元数据，不安装也不运行 CLI。
   - 出现以下情况时，job 判失败，这次定时运行变红，GitHub 会通知 Owner：
     - 出现新的大版本，且不在 `TRACKED` 表里；
     - 渠道的 latest 被标为 deprecated；
     - 版本号无法解析。
   - 同一大版本内的更新只写进摘要。codex 0.x 的次版本几乎每周都会升，同样只写进摘要，契约由 `codex-schema` 守住。
   - registry 不可用时记为 skip，不报警。
2. **契约漂移。** 上表里的三个 workflow 也是每周运行，任何一个变红都按 §4 的 L2 处理。
3. **会话开始时。** 开发会话要动 harness 相关代码时，先在本地运行 `python3 internal/cockpit/harnessdrift/releases.py`，几秒钟就能看到现状。
4. **外部消息。** 看到大版本发布、包改名或迁移指南时，即使 CI 还没报，也立即按 L3 处理，并把新渠道加进 `CHANNELS`。

## 4. 分级与响应

| 级别 | 触发 | 响应 |
|---|---|---|
| L0 本机故障 | 本机 harness 已升级，job 失败或报不支持 | 立即处理：确认版本，按 §6 回退或修复，在 Linear 建 issue |
| L1 例行更新 | 同一大版本内的新版本，契约 CI 是绿的 | 不需要处理。Owner 升级本机后运行 `pactify sub doctor <harness>` |
| L2 契约漂移 | `codex-schema`、`opencode-api` 或 `harness-drift` 变红 | 建 issue。在隔离 HOME 里复现，修改适配器，重录 golden，更新基线，按 §5 验证 |
| L3 新大版本、新包或弃用 | `releases` 报警或外部消息 | 做影响评估（§5 的 1–3 级），写 `docs/harness-v2/<harness>-<version>-impact.md`。然后由 Owner 从 A、B、C 中选一项，见表后说明 |

L3 的三种选择：

- **A. 支持新版本**：开新的 issue 改适配器。
- **B. 暂不支持**：加上 `Harness.Supported` 的快速失败，把该大版本记入 `TRACKED`，并写明重新评估的条件。
- **C. 停用这个 harness。**

影响评估必须回答这五个问题：

- pactify 依赖的每项契约是否还成立？包括：
  - 启动方式；
  - 协议或 API；
  - 事件；
  - 权限与只读；
  - 配置；
  - 凭据位置；
  - 会话隐藏；
  - 用量；
  - 模型 ID。
- 旧版本还会维护多久？官方 latest 是否已经切到新版本？
- 本机现在是否受影响？会不会自动升级？
- 不支持新版本时，用户看到的是什么报错？
- 迁移要花多少工作量？

## 5. 验证阶梯

按成本从低到高排列。L2 至少做到第 4 级；适配器改动要做到第 6 级；转正还要加上第 7 级。

| 级 | 内容 | 命令或位置 | 计费 |
|---|---|---|---|
| 1 | 离线静态检查 | `python3 internal/cockpit/harnessdrift/check.py --static`，以及各 `*_test.py` | 否 |
| 2 | 新版本在隔离 HOME 里检查参数、握手和 API（不登录、不推理） | `check.py --harness <h>`、`opencodeapi/probe.py` 与 `check.py`。新的大版本按影响报告里的探测步骤做 | 否 |
| 3 | 契约快照 | codex schema、opencode OpenAPI 投影 | 否 |
| 4 | golden 回放 | `go test ./internal/cockpit/...` | 否 |
| 5 | 本机 doctor | `pactify sub doctor <h>`。模型规则见下 | 低 |
| 6 | AJ-718 端到端集成套件（14 个场景） | `docs/harness-v2/aj718-e2e-report.md` 里的命令，要用持久的 `PACTIFY_HOME=~/.pactify-e2e/<h>` | 中 |
| 7 | 耐久大任务 3 题 | `docs/specs/harness-v2.md` §4 | 高 |

第 5–7 级的模型规则：

- opencode 用 minimax.io 官方模型，或 OpenCode Zen 的 free 模型；
- DeepSeek 只用 flash；
- agy 用 flash；
- codex 用默认模型。

## 6. 响应时的做法

- **快速失败。** 在 `internal/sub/harness.go` 中给该 harness 设置 `Supported`，job、`sub doctor` 和模型发现都会在启动 harness 进程之前失败。报错信息用英文，写明不支持的版本、原因和回退命令。示例见 `opencodeSupported`。
- **更新基线**，以下几处要和适配器一起改：
  - `versions.json`、`CODEX_VERSION`；
  - `opencode-api.yml` 的固定版本；
  - `releases.py` 的 `CHANNELS` 与 `TRACKED`；
  - 本文 §2 的表格。
- **回退本机的 CLI。** 只在 Owner 确认后执行。安装 1.x 的命令写在影响报告里，执行后运行 `pactify sub doctor <h>`。
- **凭据。** 探测都在隔离 HOME 里进行，最多用合成的测试值，不读取也不复制真实凭据。真实运行的注意事项见 AJ-718 报告的凭据刷新一节。

## 7. 记录

- **Linear issue**：第一行写 `Confirmed:` 或 `Request:`。L3 的选择由 Owner 做出后，记在 issue 里。
- **影响报告**：放在 `docs/harness-v2/`。
- **`docs/CHANGELOG.md`**：在 Unreleased 下写一节，说明改了什么、为什么改、跑了哪些门。
- **`TRACKED` 条目**：引用对应的 issue。支持新版本后删除该条目。
