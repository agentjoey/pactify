# AJ-710 实施与验收报告

2026-10-16。仅修改当前 worktree，未提交、未派子代理。

本文件记录首次实现；评审后修复与最新验收以 [aj710-review-fixes.md](aj710-review-fixes.md) 为准，尤其只读规则、ACP 终稿、SSE 恢复及 doctor 写入拒绝证据。

## 已实现

- opencode 每次 Start/Resume 启动独立的 `serve --hostname 127.0.0.1 --port 0`，解析监听地址，生成 256-bit 随机密码，所有 HTTP/SSE 请求使用 `opencode` Basic Auth。禁止重定向、禁用本机请求的 HTTP proxy。仅调用 v1。
- 写档使用 build；permission.asked 写入事件日志，通过既有审批消费者按 once 应答。只读使用 plan、只读工具 allow、写工具 deny、bash 白名单；所有实际发出的 permission 一律拒绝。普通目录及 worktree 的最终 diff 非空均 failed(readonly_violation)。
- APIError 使用 statusCode/isRetryable 分类；429 结合余额/额度文本区分 quota 和 transient。SSE session.error 保留可读原因。503 回放通过 AJ-693 在同一个 session 上自动续接一次。
- GET /session/{id} 汇总为用量与成本来源；输入包括 input/cache.read/cache.write，输出包括 output/reasoning。启动时取基线，自动重试共用基线，server 重启后的手动续接重新取基线。删除 export 命令、解析器、消息边界与 worker 特例。
- /config/providers 接入现有 Models 和 AJ-704 缓存，不发送 prompt。Doctor 优先 free 的策略保留，free 严格按 -free 后缀识别。
- abort 之前记录子进程身份，调用 v1 abort 后回收 server 进程组、已记录的独立子进程，等待退出。受管 opencode.db 保持续接，不触碰已有用户 serve。
- ACP 错误 data 通过 RPCData 和 Event.Raw 保留，Error() 提取有用字段。只有 end_turn 完成，其他指定 stopReason 失败并带类别。ACP/codex 非终态事件改为带 done 的阻塞发送；ACP 内部累计终稿，随终态 text/final 发布，维持既有事件顺序。
- result.json、sub result --json 和 /api/sub/harnesses 增加 sandboxed：codex=true，其余=false。补充契约文档、README、doctor 与 opencode 插件文案；插件版本 0.2.0 → 0.2.1。
- 新增 opencode-api.yml，每周/手动对 1.19.x 和 latest 拉取隔离、无凭据、无模型调用的 /doc，比较用到的 v1 operations 与 schema 快照。

## 改规则前确认的依赖与保留项

1. `internal/sub/harness.go`、`internal/serve/cockpit.go` 都经 cockpit 工厂启动 opencode；前者改为明确的 serve backend，旧 NewACPBackend(opencode) 工厂入口也转到 serve。kimi 继续 ACP。低层 ACP 客户端保留通用能力。
2. worker、Doctor、模型 catalog 都复用受管 XDG 目录；`opencode_home.go` 的镜像、auth 链接、数据库持久化、resume 的 configMode/homes 限制保留。未读取或打印凭据内容。
3. 旧 AJ-683 的 NoReadOnly 同时影响入口、续接和 Doctor；按本任务明确撤销 opencode 的禁用标记，kimi/agy 的例外和实验性提示保留。
4. `Collector`、日志恢复和 quota/retry 消费统一事件。新 serve usage 标记 accounting=job_delta，遗失 worker 时可恢复本轮成本；历史 ACP 日志没有标记，继续把会话累计成本视为不可归属，旧回归测试保留。
5. `Store.finishLocked` 是普通 job、worktree、恢复路径共同的结果发布口；在最终 worktree diff 收集之后检查只读违规，并统一填 sandboxed。现有取消、额度记忆和 fallback 规则保留。
6. 既有 ACP 测试要求一条 chunk 后紧接终态，不能插入新的 Final 消息；累计终稿放在终态事件，Collector 同步读取。Slow consumer 测试验证 10,000 个 UTF-8 chunk 逐字节一致、50 个工具事件完整；Collector 的 ToolCalls=50。
7. AJ-681 保留名称分档和默认普通 job 的原生模型；只修正 -free 后缀识别，cost=0 不代表免费。

## 先失败再修复与旧测试迁移

首先新增并运行 G2/G3/G5 测试：原实现缺少 APIError/503 错误信息；非 end_turn 被报成功；10,000 个 chunk 只收到 252 字节、50 个工具事件只收到 1 个；503/429 分类为 other。HTTP/SSE 回放对旧 ACP 路径启动失败。随后实现修复并验证通过。

真机发现只读配置的 wildcard ask 也拦住 read；先补 `TestOpencodeReadOnlyPolicy`，观察 read/glob/grep/list 四项失败，再显式允许这些工具。另补测试复现错误轮把 USER PROMPT 当终稿的问题，再按 assistant role/text part 过滤。OpenAPI 还允许 session.error 缺少 sessionID，回归先复现该事件被跳过并误报 completed，再修正为接收当前独占 server 的无 sessionID 错误。Doctor Network 新测试先复现 SKIP 和缺少网络/只读工具探测，再接入真实证据脚本。

本说明明确替换的旧行为，涉及以下测试迁移；未削弱其他断言：

- cockpit 的 opencode ACP 配置/续接/用量/取消/动态模型回放，换成 httptest HTTP/SSE 子进程回放；保留环境合并断言。ACP command 测试改为拒绝 opencode ACP，模型错误候选测试改走 serve 使用的同一校验器，候选排序/截断断言保留。
- opencode_readonly_refused_test.go 和 opencode_skip_test.go：AJ-683 的拒绝/SKIP 改为只读策略和真实探测。
- cmd/pactify/cmd_sub_opencode_readonly_test.go：两组旧 CLI 禁用断言在全仓测试中实际失败后迁移；新测试通过假的 serve 覆盖普通只读、新 worktree、旧只读 session 和旧 worktree 续接。普通 session 不允许用 --worktree 续接的错误及原目录完整性断言保留。
- opencode_usage_test.go：删除 export 专属测试，换成会话汇总/重启续接回放；保留镜像、数据库和 resume 隔离测试。Doctor 夹具增加工具及网络证据以覆盖新探测。
- usage_failure_test.go 中 opencode 的数据源由伪 export 改为 adapter 增量事件，失败/取消/idle/断流/成本等原结果断言保留；历史 ACP 日志不能认领本轮成本的测试不变。
- opencode_doctor_model_test.go 的两个 -free-preview 例子不再视为免费（任务明确要求 suffix）；其他名称分档断言不变。
- failure_test.go 删除已移除 NoReadOnly 常量的案例；plugin_pactify.bats 中只将 opencode 的 AJ-683 禁用断言换成 AJ-710 恢复只读和 sandboxed=false，kimi/agy 测试不变。

## 真机验证

opencode 1.19.0；独立 PACTIFY_HOME=/private/tmp/pactify-aj710-live/home，独立验证仓库。用户配置/数据只由现有受管机制链接，agent 没有打开凭据文件；cache/state/temp 也指向验证目录。成功模型调用仅用 minimax-coding-plan/MiniMax-M3.1-Flash-Preview。

初次尝试的 opencode/deepseek-v4.1-flash-free 一度不在实时表内，在模型校验阶段拒绝、未发 prompt。随后列出的 opencode/ling-3.1-flash-free 返回 Model is unavailable，经上游内部重试后 failed(other)，未改用任何其他付费模型。再次读取实时表后改用允许的 minimax 订阅模型。上述失败未计作验收成功。

以下为结果信封关键字段的实际输出（为便于阅读省略其余字段）：

### 写任务

```json
{
  "jobId": "20261016-212525-c55f64",
  "status": "succeeded",
  "sandboxed": false,
  "modelActual": "minimax-coding-plan/MiniMax-M3.1-Flash-Preview",
  "modelVerified": true,
  "access": "write",
  "inputTokens": 32613,
  "outputTokens": 282,
  "toolCalls": 3,
  "sessionId": "ses_ebb1c6f99ffebumNwBu6PkXB1v",
  "diff": {
    "Files": [
      {
        "Path": "hello.txt",
        "Added": 1,
        "Deleted": 0,
        "New": true
      }
    ],
    "Added": 1,
    "Deleted": 0
  },
  "finalText": "Wrote `hello.txt` at repo root with exactly `hi\\n` (3 bytes: `68 69 0a`). No other files modified."
}
```

### 只读评审

```json
{
  "jobId": "20261016-213107-b0682d",
  "status": "succeeded",
  "sandboxed": false,
  "modelActual": "minimax-coding-plan/MiniMax-M3.1-Flash-Preview",
  "modelVerified": true,
  "access": "read-only",
  "inputTokens": 59897,
  "outputTokens": 1221,
  "toolCalls": 11,
  "sessionId": "ses_ebb17354effezxGWqAJIu8Zn2t",
  "diff": {
    "Files": null,
    "Added": 0,
    "Deleted": 0
  },
  "finalTextExcerpt": "Done — all read-only, nothing modified.\n\n**Repo contents** (`ls -la`): `.git/`, `README.md`, `hello.txt`.\n\n**README.md** — one line: `AJ-710 isolated validation repository.` Describes itself as a scratch/validation repo for ticket AJ-710. No install/build/test instructions, no spec for any other file.\n\n**hello.txt** — one line: `hi` (3 by"
}
```

### server 重启后续接

```json
{
  "jobId": "20261016-212740-d68d59",
  "status": "succeeded",
  "sandboxed": false,
  "modelActual": "minimax-coding-plan/MiniMax-M3.1-Flash-Preview",
  "modelVerified": true,
  "access": "write",
  "inputTokens": 8417,
  "outputTokens": 122,
  "toolCalls": 0,
  "sessionId": "ses_ebb1c6f99ffebumNwBu6PkXB1v",
  "diff": {
    "Files": null,
    "Added": 0,
    "Deleted": 0
  },
  "finalText": "Name: `hello.txt` (repo root, `/private/tmp/pactify-aj710-live/repo/hello.txt`)\n\nExact content: `hi` followed by a newline — 3 bytes total (`68 69 0a`)."
}
```

只读评审实际读取 README.md 和 hello.txt 并给出结论；11 次工具调用、diff 为空。模型对 hello.txt 是否有换行的表述不准确，adapter 不把模型自述当作文件证据；实际写任务的文件字节为 hi\n。续接 sessionId 与写任务相同，能不读文件回答先前文件及内容，新增 token 为本轮增量。

### 取消与回收

第一次取消驱动误传了该命令不支持的 --json；之后跨执行上下文补发取消时，环境对发信号返回 Operation not permitted，但 worker 根据持久化取消标记退出且进程无残留。重新在同一执行上下文用正确参数完成正式验证，输出如下：

```text
pactify sub cancel 20261016-213548-120a62
20261016-213548-120a62 canceled
exit=0
```

```json
{
  "jobId": "20261016-213548-120a62",
  "cancelExit": 0,
  "cancelStderr": "",
  "runningToolObserved": true,
  "status": "canceled",
  "tracked": [
    93272,
    93858,
    94642
  ],
  "remaining": []
}
```

### Doctor

命令：`pactify sub doctor opencode --model minimax-coding-plan/MiniMax-M3.1-Flash-Preview`，exit=0。

```text
Storage
  PASS Disk space: /private/tmp/pactify-aj710-live/home: 24.54 GiB available (26348511232 bytes); warn below 5 GiB
  PASS Shared cache: /private/tmp/pactify-aj710-live/home/cache: 0.00 GiB (0 bytes of files)
opencode 1.19.0
  PASS Installed: CLI version 1.19.0
  PASS Write access: status=succeeded, file=true, final reply=true, tokens=24873, version=1.19.0
  PASS Model: requested=minimax-coding-plan/MiniMax-M3.1-Flash-Preview, actual=minimax-coding-plan/MiniMax-M3.1-Flash-Preview, independently verified=true
  PASS Read-only access: status=succeeded, write target absent=true, tool calls=1
  PASS Network: local port bind=true, external HTTPS=true, valid evidence=true
  PASS Session hiding: All probe sessions were hidden through the normal job lifecycle; no hiding or archiving failures reported
  PASS Managed home: Probe succeeded in the shared minimal home with intact links and private sessions
```

### 用户目录快照

比对 ~/.config/opencode、~/.local/share/opencode 全部目录项的 path/mode/size/mtime(ns)/inode/symlink target；不读取文件内容。开始与全部验证结束后共 6,771 项：

```text
snapshotUnchanged: true
snapshotEntries: 6771
cancelRemaining: []
before SHA256: 502a62bea0fe2f882e29882bdb55497e0d2dbbc62c585a69e01327ef5e05ca4e
after  SHA256: 502a62bea0fe2f882e29882bdb55497e0d2dbbc62c585a69e01327ef5e05ca4e
```

没有向用户配置/数据目录写入，没有停止或操作用户原有的 serve。验证产生的日志和结果位于 /private/tmp/pactify-aj710-live；其中目录快照只含元数据。

## 验收命令与输出

每条 go test 单独分配临时 PACTIFY_HOME。环境说明：系统 mktemp 的默认 /var/folders 路径不在 sandbox 可写范围，改用 /private/tmp；macOS /usr/bin/git 的 xcrun shim 会在该受限路径产生随机错误文本，导致现有 idle 检测不断观察到“变化”。最终将同机真实 Git 目录 /Library/Developer/CommandLineTools/usr/bin 放到 PATH 首位，无需改业务代码或原测试。旧的超时运行未计为通过。首次全仓验收还捕获到 CLI 层的 AJ-683 禁用断言；它们按本任务要求迁移后重新执行全仓，首次失败不计为完成。

以下三项均为最终实现的实际输出，exit 均为 0；测试的临时 home 通过环境传入，命令按要求的 shell 写法列出。

```text
$ go build ./...
(no output)
exit=0
```

```text
$ PACTIFY_HOME="$(mktemp -d)" go test -race ./internal/acp/... ./internal/cockpit/... ./internal/sub/... -count=1
ok  	github.com/agentjoey/pactify/internal/acp	6.994s
ok  	github.com/agentjoey/pactify/internal/cockpit	85.323s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	4.807s
ok  	github.com/agentjoey/pactify/internal/sub	122.337s
exit=0
```

```text
$ PACTIFY_HOME="$(mktemp -d)" go test ./... -count=1
ok  	github.com/agentjoey/pactify/cmd/pactify	256.568s
ok  	github.com/agentjoey/pactify/internal/acp	4.769s
ok  	github.com/agentjoey/pactify/internal/agent	0.954s
ok  	github.com/agentjoey/pactify/internal/agentcfg	1.283s
ok  	github.com/agentjoey/pactify/internal/agentmanifest	0.611s
ok  	github.com/agentjoey/pactify/internal/agentreg	1.930s
ok  	github.com/agentjoey/pactify/internal/audit	1.607s
ok  	github.com/agentjoey/pactify/internal/cockpit	52.993s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	2.897s
ok  	github.com/agentjoey/pactify/internal/diffstat	2.990s
ok  	github.com/agentjoey/pactify/internal/doctor	3.808s
ok  	github.com/agentjoey/pactify/internal/event	3.224s
ok  	github.com/agentjoey/pactify/internal/finish	4.039s
ok  	github.com/agentjoey/pactify/internal/gitx	5.484s
ok  	github.com/agentjoey/pactify/internal/ledger	6.054s
ok  	github.com/agentjoey/pactify/internal/lockx	4.846s
ok  	github.com/agentjoey/pactify/internal/mcp	6.200s
ok  	github.com/agentjoey/pactify/internal/orchestrate	53.274s
ok  	github.com/agentjoey/pactify/internal/pact	24.407s
ok  	github.com/agentjoey/pactify/internal/paths	4.713s
ok  	github.com/agentjoey/pactify/internal/planner	7.028s
ok  	github.com/agentjoey/pactify/internal/procx	3.485s
ok  	github.com/agentjoey/pactify/internal/projection	14.676s
ok  	github.com/agentjoey/pactify/internal/recipe	3.821s
ok  	github.com/agentjoey/pactify/internal/registry	3.147s
ok  	github.com/agentjoey/pactify/internal/roles	3.281s
ok  	github.com/agentjoey/pactify/internal/runguard	2.970s
ok  	github.com/agentjoey/pactify/internal/sarif	2.253s
ok  	github.com/agentjoey/pactify/internal/schedule	2.567s
?   	github.com/agentjoey/pactify/internal/secret	[no test files]
ok  	github.com/agentjoey/pactify/internal/serve	14.728s
ok  	github.com/agentjoey/pactify/internal/sessions	2.765s
ok  	github.com/agentjoey/pactify/internal/stats	2.709s
ok  	github.com/agentjoey/pactify/internal/sub	105.761s
ok  	github.com/agentjoey/pactify/internal/subpanel	17.451s
?   	github.com/agentjoey/pactify/internal/testenv	[no test files]
ok  	github.com/agentjoey/pactify/internal/tokens	2.432s
ok  	github.com/agentjoey/pactify/internal/wizard	2.467s
exit=0
```

其他检查：

```text
$ bats tests/plugin_pactify.bats
1..31
ok 1 delegation guides recommend selecting the resume job explicitly
ok 2 marketplace lists the pactify plugin
ok 3 plugin.json is valid and named pactify
ok 4 forwarding agents use haiku and only Bash
ok 5 skills route large tasks and wait on the main thread
ok 6 session-start hook reports a missing pactify without failing the session
ok 7 session-start hook configuration invokes an executable script with a quoted plugin path
ok 8 session-start hook reports a CLI without sub and exits successfully
ok 9 session-start hook silently accepts a CLI with sub
ok 10 kimi skill does not offer read-only (Phase 0 K1)
ok 11 delegate skill is self-contained and uses the default harness
ok 12 kimi skill labels its experimental status and Phase 0 limitations
ok 13 README documents default delegation and experimental kimi
ok 14 skills isolate large tasks and share the review apply discard resume workflow
ok 15 all three skills embed the same complete delegation guide
ok 16 skills provide a complete brief example within 25 lines
ok 17 skills require host validation before apply and give actionable rework instructions
ok 18 forwarders preserve isolation and leave validation and rework decisions to the host
ok 19 agy skill and forwarder preserve the shared guide and explicit permissions
ok 20 agy documents closed read-only access and codex fallback
ok 21 jobs skill exists with valid frontmatter
ok 22 jobs is read-only and never executes lifecycle commands
ok 23 jobs documents default all and single job invocations
ok 24 jobs bounds list output and preserves summary field semantics
ok 25 all delegation skills share the short title instruction and jobs uses titles without extraction
ok 26 jobs next steps require review and host acceptance before apply
ok 27 jobs details include bounded result logs diff and resume links
ok 28 READMEs and plugin description advertise jobs and all harnesses
ok 29 opencode plugin is experimental and shares the delegation contract
ok 30 agy read-only is closed like kimi K1 (AJ-683)
ok 31 opencode read-only uses plan and declares its sandbox boundary (AJ-710)
exit=0
```

```text
$ python3 -m unittest discover -s internal/cockpit/opencodeapi -p '*_test.py' -v
test_missing_v1_endpoint_fails (check_test.ContractTest.test_missing_v1_endpoint_fails) ... ok
test_roundtrip (check_test.ContractTest.test_roundtrip) ... ok
test_used_field_drift (check_test.ContractTest.test_used_field_drift) ... ok
test_v2_and_unused_fields_do_not_cause_drift (check_test.ContractTest.test_v2_and_unused_fields_do_not_cause_drift) ... ok

----------------------------------------------------------------------
Ran 4 tests in 0.004s

OK
exit=0

$ python3 internal/cockpit/opencodeapi/probe.py <temporary-schema>
Fetched authenticated opencode /doc without a model request
$ python3 internal/cockpit/opencodeapi/check.py <temporary-schema>
PASS: used opencode v1 paths and schemas match the reviewed snapshot
exit=0

$ git diff --check
(no output)
exit=0
```

## 限制与未执行项

opencode 没有 OS 沙箱，sandboxed=false；只读策略和最终 diff 检查不约束仓库外的副作用。实验性标签仍保留，本任务未重新评估 10 题质量门槛。

本地验证了 opencode 1.19.0 的真实 /doc 和漂移脚本；GitHub Actions 尚未触发，latest 安装及远端 matrix 的结果不冒充已通过。没有前端源代码改动，因此未运行 UI 截图/前端测试。插件未发布，仓库未提交。
