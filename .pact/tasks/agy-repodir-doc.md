# agy-repodir-doc — 给 `{repoDir}` 补上与 `{seat}` 对等的文档与测试

tier: L0
verify: go test ./internal/agentmanifest/ ./internal/orchestrate/ -run 'Render|RepoDir|Manifest'
dimension: correctness

## 背景 / Context

`orchestrate` 的 runner 在 exec 时会对 argv 做整词替换，目前有三个占位符：
`{briefing}`、`{seat}`、`{repoDir}`（`internal/orchestrate/runner.go` 的 switch）。

前两个在 `internal/agentmanifest/` 里都有交代：
- `render.go` 顶部的注释列出了 `{seat}` 的行为（「left literal — orchestrate 的 runner 在 exec 时替换」）
- `render_test.go` 有对应的透传测试
- `manifest.go` 的 `Validate` 还对 `{seat}` 做了校验

`{repoDir}` 是本次 antigravity 接入新增的，**三者都没有**。后果：用户自写的 custom-agent
manifest 里如果 argv 恰好出现字面量 `{repoDir}`，会被静默改写成仓库路径，而 schema 文档里
查不到这个词。

## 目标 / Goal

**只做文档与测试，不改任何运行时行为。**

1. `internal/agentmanifest/render.go` 顶部的占位符说明里补上 `{repoDir}` 一行，
   与既有 `{seat}` 那行同样的写法和口径（说明它同样是 left literal，由 orchestrate 的
   runner 在 exec 时替换为该 stint 的仓库目录）。
2. `internal/agentmanifest/render_test.go` 补一个测试，钉死 `RenderArgs` 对 `{repoDir}`
   **原样透传**（不替换、不丢弃），写法参照同文件里已有的 `{seat}` 透传断言。

## 不要做 / Out of scope

- **不要**给 `{repoDir}` 加 `Validate` 校验（`{seat}` 的校验是因为 `identity.via=arg`
  依赖它；`{repoDir}` 没有对应的强制场景，加了会拒掉现存的合法 manifest）。
- **不要**改 `runner.go`、`launch.go` 或任何运行时替换逻辑。
- **不要**改既有测试的断言。
- **不要**改 `{seat}` / `{briefing}` 的任何说明。

## 验收 / Acceptance

评审维度：**correctness**。

- `render.go` 的占位符注释块中出现 `{repoDir}`，且说明与实际行为一致
  （整词匹配、原样透传给 orchestrate、由 runner 替换为 stint 的 RepoDir）。
- 新增测试：argv 含 `{repoDir}` 时 `RenderArgs` 的输出**逐元素等于**输入中的该元素
  （即未被 agentmanifest 层替换）。测试必须在移除该行为时会红。
- `Validate` 的行为逐字节不变（不得新增校验规则）。
- 门绿：`go test ./internal/agentmanifest/ ./internal/orchestrate/ -run 'Render|RepoDir|Manifest'`
- 全包不回归：`go test ./internal/agentmanifest/`
