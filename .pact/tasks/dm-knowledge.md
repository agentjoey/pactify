# dm-knowledge — git-native 知识层(skills + memory 注入 briefing)

> 母 spec:`docs/specs/driver-modernization.md` §3。无依赖,可即刻开工。

## 交付
1. `internal/orchestrate/knowledge.go`:
   - 读 `.pact/memory.md`(存在则全文)。
   - 读 `.pact/skills/*.md`:手写 frontmatter 解析(`---` 定界;`roles: [a, b]` / `keywords: [x, y]` 两个 key,逐行解析,别引 yaml 依赖)。role 匹配(worker/reviewer,空=全部)+ keyword 命中(任务 spec 路径名/briefing 里包含任一词,大小写不敏感;空=总是)。
   - `KnowledgeFor(role string, taskHint string) string`:拼「## 项目知识」节,memory 先、skills 后,合计 4KB 截断(截断处加 `…(truncated)`)。malformed frontmatter → 跳过该文件,不报错。
2. `brief.go` 集成:workerBrief / reviewerBrief 尾部追加该节(空则完全不加,briefing 字节不变)。

## 测试
role 过滤、keyword 命中/不命中、4KB 截断、malformed 跳过、无文件=briefing 与现状逐字节相同(golden)。`go test ./internal/orchestrate/ -run 'Brief|Knowledge'` 绿。

## 边界
不做 memory 自动提炼;不做 web UI;不动 planner prompt。
