# agy 1.4.0 / opencode 1.19.0 集成假设探针报告（2026-10-16）

原始产物目录：`scratchpad/probes/`（`out/*.jsonl|err|log`、`agy-calls.log`、`oc-calls.log`、`snap-*.txt`、驱动脚本 `a3.py a6.py oc1.py o2b.py o3.py o4.py o5.py o6.py`）。

## 隔离与用量

- **agy 隔离**：`HOME=probes/agyhome`，其中只有 `.gemini/antigravity-cli/antigravity-oauth-token`，且是**复制**，不是符号链接。复制比 pactify 的符号链接更严：真实 token 的 mtime 为 10-16 06:49，说明经符号链接的刷新会写回真实文件。工作目录是一次性仓库 `probes/repo`。
- **opencode 隔离**：`env -i`，`HOME / XDG_DATA_HOME / XDG_CONFIG_HOME / XDG_CACHE_HOME / XDG_STATE_HOME` 全部指向 `probes/oc/*`。`auth.json` 为复制件。另设 `OPENCODE_DISABLE_CLAUDE_CODE=1 OPENCODE_DISABLE_EXTERNAL_SKILLS=1 OPENCODE_PURE=1 OPENCODE_DISABLE_AUTOUPDATE=1`，与 pactify `OpencodeEnv` 的 MinimalConfig 一致。工作目录是一次性仓库 `probes/ocrepo`。
- **快照**：对 `~/.gemini ~/.antigravity ~/.cache/antigravity ~/.config/opencode ~/.local/share/opencode ~/.cache/opencode ~/.local/state/opencode ~/.opencode` 记录 `path size mtime(ns) perms` 的 shasum 与全量清单。前后 `diff` 为空，**结论：用户真实目录未改动。** pactify 仓库 `git status` 与开始时一致。
- **agy 用量**：
  - 计费轮次 11 次，全部为 `gemini-3.8-flash-low`：A1×4、A2×2、A3×2（同一进程两轮）、A4×2、A6×1。
  - **违规 1 次（如实报告）**：调查 `/usage` 时运行了 `agy -p "/usage" --disable-slash-commands --print-timeout 1s`，没有带 `--model`。日志显示它落到了默认模型 `Gemini 4 Argon (Medium)`。1 秒后被 print-timeout 截断，返回 usage 全为 0；Argon 周额度此前已是 0%。
  - 另有 2 次 `/usage` 和 2 次错误探针（坏 `--model`、坏 stdin 行），都没有调用模型，`num_turns:0`。
- **opencode 用量**：prompt 共 9 次。
  - 真正调用免费模型 `opencode/deepseek-v4.1-flash-free` 的 4 次：O2b、O3、O5、O6。
  - 1 次 v2 prompt 被接收但没有执行。
  - 2 次坏 model/provider，未发出请求。
  - 2 次发给本地假 provider（`127.0.0.1:47399`）。
  - 没有使用任何付费模型。
- **进程**：我启动的 opencode serve（:47311、:47312）、`fake.py` 和 agy 子进程已全部结束。`pid 1268 opencode serve --port=4096` 是用户自己原有的进程，没有动。

---

## AGY

### A1 `--mode plan`

**A1a 写文件请求**
- 命令：`HOME=agyhome agy -p "Create a file x.txt containing hi in the current directory." --mode plan --model gemini-3.8-flash-low --output-format stream-json`
- 结果：
  - init 中 `permission_mode:"request-review"`，`expanded_commands:[{"name":"plan"}]`。
  - 它没有拒绝，而是用 `write_to_file` 把 `plan.md` 写进 `agyhome/.gemini/antigravity-cli/brain/<conv>/plan.md`，即 artifact 目录，不在仓库内。
  - 最终回答为 "I have created an implementation plan … let me know if you approve"，`result.status:"SUCCESS"`，exit 0，仓库无变化。
- **结论：confirmed**。plan 模式不会立即结束，会产出计划并以 SUCCESS 收尾，仓库不变。

**A1b 要求跑 `git log` / `ls`**
- 结果：模型根本没有调用 `run_command`，只写了 `plan.md` 并请求 "click Proceed"。

**A1c 纯问题（"Investigate with read-only tools and answer directly"）**
- 模型调用了 `run_command "git rev-list --count HEAD && git ls-files"`，该步 `state:"ERROR"`。
- 错误信息：`permission check failed for unsandboxed "…": user denied permission … Do not attempt to circumvent`。
- 回合**立即结束**：`result.status:"SUCCESS"`，`response:""`，`denied_actions:[{"action":"command","display_name":"RunCommand"}]`，exit 0。
- stderr：`jetski: no output produced — a tool required the "command" permission that headless mode cannot prompt for, so it was auto-denied. Add an allow-rule under permissions.allow in settings.json (e.g. command(<target>)) …`
- **结论：refuted**。plan 模式下，无头运行时所有 shell 都会被自动拒绝，而且拒绝后会以空回答结束。这正是 "read-only never converged" 的根因之一。

**A1d 加 allow 规则后重跑 A1c**
- 在私有 home 写入 `agyhome/.gemini/antigravity-cli/settings.json`：
  ```json
  {"permissions":{"allow":["command(git rev-list)","command(git ls-files)","command(git log)","command(ls)"]}}
  ```
- 结果：`run_command "git rev-list --count HEAD && git ls-files"` 为 DONE，输出 `1\r\nREADME.md`。最终回答 "has **1 commit** … README.md"，状态 SUCCESS。
- 日志：`CLI settings initialized: permissions=&{Allow:[…] Deny:[] Ask:[]}`。
- **结论：confirmed**。`permissions.allow` 的前缀规则对 `a && b` 复合命令逐段匹配后放行。
- **对 pactify 的影响**：agy 的只读模式可以这样做：`--mode plan`（或默认 request-review）加上在私有 home 的 `settings.json` 写只读命令白名单，例如 `command(git log)`、`command(git diff)`、`command(rg)`、`command(ls)`。拒绝时要读 `result.denied_actions` 和空 `response`，判为"未收敛"，不能当作 SUCCESS。

### A2 Hooks

- **位置与 schema**：来自二进制内嵌文档 "Lifecycle Hooks (hooks.json)"。
  - 全局位置：`~/.gemini/config/hooks.json`。工作区位置：`.agents/hooks.json`（也可以是 `.agent/`、`_agents/`），从 cwd 向上找到仓库根。
  - 格式：`{"<hook名>":{"enabled":bool,"PreToolUse":[{"matcher":"regex","hooks":[{"type":"command","command":"…","timeout":秒}]}],"PostToolUse":[…],"SessionStart":[handler…],"PreInvocation":[…],"PostInvocation":[…],"Stop":[handler…]}}`。
  - stdin 用 camelCase JSON：`conversationId`、`workspacePaths`、`transcriptPath`、`artifactDirectoryPath`、`modelName`，以及 `toolCall{name,args}`、`stepIdx`。
  - PreToolUse 的输出：`{"decision":"allow|deny|ask|force_ask","reason":…,"permissionOverrides":[…],"overwrite":{…}}`。
  - Stop 的输出：`{"decision":"continue","reason":…}`。
  - PostInvocation 可以返回 `terminationBehavior: force_continue|terminate`。
  - 由于 pactify 的 agy 私有 home 排除了 `.gemini`，`<managed HOME>/.gemini/config/hooks.json` 只作用于委派会话，不影响用户。
  - 首次运行时 agy 会在私有 home 自建 `.gemini/config/{mcp_config.json,projects/,.migrated}`。

**A2a：PreToolUse 拒绝文件写**
- 写入 `agyhome/.gemini/config/hooks.json`，matcher `*`，hook 脚本对 write 类工具返回 `{"decision":"deny","reason":"pactify probe: file writes are blocked in this session"}`。
- 命令：`agy -p "Create a file x.txt containing hi…" --dangerously-skip-permissions --model gemini-3.8-flash-low --output-format stream-json`
- 结果：
  - `write_to_file` 一步为 `state:"ERROR"`，`error:{"type":"TOOL_ERROR","message":"tool call denied by pre-tool hook: pactify probe: …"}`。
  - **回合没有结束**。模型继续给出回答 "I attempted to create x.txt, but … blocked by an active pre-tool hook"，状态 SUCCESS，仓库无变化。
  - 它也没有改用 `run_command` 绕过。
  - 在 `--dangerously-skip-permissions` 下 hook 依然生效。
  - hook 收到的 payload 含完整参数：`toolCall.args.{CodeContent,TargetFile,Overwrite,…}`。
- **结论：confirmed**。拒绝后 agent 会继续并给出结论。

**A2b：Stop hook 返回 continue（只在第一次返回）**
- 结果：
  - 第一次 Stop 的 payload：`{"executionNum":0,"terminationReason":"NO_TOOL_CALL","fullyIdle":true,"error":"",…}`。
  - 之后出现 `step_type:"system_message"`，即 reason 被注入。
  - 模型再答一次，第二次 Stop 的 payload 为 `executionNum:1`。
  - `result.response:"hello\nhello\nBANANA\n"`，`num_turns:1`。
- **结论：confirmed**。Stop hook 可以阻止结束并注入指令。
- **对 pactify 的影响**：可以用 Stop hook 实现"必须给出结论或 verified 摘要才允许停"。注意 `result.response` 会把所有 agent_response 拼接起来；pactify 现在取最后一段有内容的 step，做法正确。

### A3 单进程多轮 `--input-format stream-json`

- 驱动脚本：`a3.py`。启动命令为 `agy --input-format stream-json --output-format stream-json --model gemini-3.8-flash-low --dangerously-skip-permissions`。每轮写入 `{"event":"user","message":{"content":"…"}}\n`，等收到 `result` 后再写下一行。
- 结果：
  - 同一个 pid（97893）。init 只出现 1 次。两轮的 `conversation_id` 都是 `cbe94957-…`。第二轮正确答出 "42"。
  - 两轮后进程仍存活；关闭 stdin 后以 exit 0 退出。
- **事件形状**：
  - `init`：`{conversation_id, init:{model,cwd,tools[60],permission_mode,expanded_commands?}}`
  - `step_update`：`{conversation_id,step_index,state:ACTIVE|DONE|ERROR,step_type:user_input|agent_response|tool|system_message, text_delta?, tool_name?, tool_info{name,parameters,output?,error{type,message}?}, duration_seconds?, usage{input_tokens,output_tokens,thinking_tokens,cache_read_tokens,total_tokens}?}`
  - `result`：`{conversation_id,status:SUCCESS|ERROR,response,error?,duration_seconds,num_turns,usage{…},denied_actions?}`
  - `command_result`：只在 slash 命令时出现。
- **关键事实：`result.usage`、`duration_seconds` 和 `num_turns` 在同一进程内是累计值。**
  - 第二轮 `input_tokens:24510` 等于两步之和 12211+12299，`num_turns:2`。
  - 逐轮用量应以每个 `agent_response` 的 DONE `usage` 求和；pactify 的 cockpit 现在就是这样做的，正确。
- **错误形状**：
  - 坏 `--model`：只输出 `{"event":"result","result":{"conversation_id":"","status":"ERROR","error":"invalid model selection … Available models: …"}}`，exit 1。
  - stdin 有一行非 JSON：先输出 init，然后 `result.status:"ERROR"`，`error:"failed to decode stream input: …"`，exit 1，**整个进程退出**。
  - 没有结构化错误码字段，只有 `error` 字符串。
- **结论：confirmed**。
- **对 pactify 的影响**：可以从"每轮一个进程"改为常驻进程，用于续接或追问；用量只能取 step 级 usage 的差分。

### A4 `--sandbox`

**A4 第一次运行**
- 命令：`--sandbox --dangerously-skip-permissions --model gemini-3.8-flash-low`，5 条命令分别执行。
- 结果：
  - 1) `curl https://example.com` 返回 `200`：**网络未被封锁**。
  - 2) 在 127.0.0.1 上 bind/listen 输出 `bound ('127.0.0.1', 60554)`：**端口未被封锁**。
  - 3) touch 工作区外路径：`Operation not permitted`。
  - 4) `GOCACHE`（在 `HOME/Library/Caches/go-build` 下）的 mkdir：`Operation not permitted`。
  - 5) `echo hi > y.txt && git commit`：`zsh:1: operation not permitted: y.txt`，**工作区内的写入也被拒绝**。
- 日志：`Print mode: enabling terminal sandbox`，`SANDBOX_COMMAND_BLOCKED`。

**A4b**
- 在 `settings.json` 加入 `trustedWorkspaces:[repo]` 后重跑第 5 条：
  - 第一次仍然 `operation not permitted`。
  - **随后模型自动重试，并带上 `BypassSandbox:true`**，见 `brain/<conv>/.system_generated/logs/transcript_full.jsonl` 中的 `"args":{"BypassSandbox":true,…}`。
  - 由于 `--dangerously-skip-permissions` 会自动批准，结果 `[main f6d9f04] probe … commit-ok`，提交成功。
- **stream-json 里的 `tool_info.parameters` 只给出 `CommandLine`，看不到 `BypassSandbox`。**
- **结论**：
  - 封锁网络：refuted。
  - 封锁端口：refuted。
  - 封锁工作区外写入和 HOME 下缓存写入：confirmed。
  - 沙箱内 `git commit`：被拒，因为终端沙箱连工作区写入都拒。
  - 在 skip-permissions 下沙箱可以被模型自行绕过：confirmed。
- **对 pactify 的影响**：
  - `--sandbox` 不能当安全边界，也不能单独用于写模式。
  - 若要使用，需加 PreToolUse hook，对 `toolCall.args.BypassSandbox==true` 返回 deny。hook 能看到完整 args 已在 A2a 证实，但 BypassSandbox 这一字段尚未专门实测。
  - 也可以不用 skip-permissions，改用 allow 规则。
  - 沙箱下 `go test` 写 GOCACHE 会失败，会触发 bypass 请求。

### A5 `/usage`

- 命令：`agy -p "/usage" --model gemini-3.8-flash-low --output-format json`
- 结果：`num_turns:0`，usage 全为 0，**没有调用模型**。
- 形状：
  ```
  {"status":"SUCCESS","response":"<TSV 摘要>","command":{"name":"usage","data":{"description":…,"groups":[{"name":"Gemini Models","description":…,"buckets":[{"id":"gemini-weekly","name":…,"window":"weekly","remaining_fraction":0.9627,"reset_time":"2026-10-23T00:40:01Z","description":…},{"id":"gemini-5h","window":"5h",…}]},{"name":"Claude and GPT models",…},{"name":"Gemini 4 Argon","buckets":[{"id":"gemini-argon-wtu-weekly","remaining_fraction":0,…}]}]}}}
  ```
- stream-json 下先发 `{"event":"command_result","command":{…}}`，再发 `result`。
- **结论：confirmed**。可用于零成本的配额检查。
- **附带事实**：
  - 私有 home 不带 `--model` 时，默认模型是 **Gemini 4 Argon (Medium)**。pactify 必须总是显式传 `--model`。
  - `--print-timeout` 到期时 exit 0，`status:"SUCCESS"`，`response:""`，stderr 为 `print timeout … returning partial output`。这容易被误判为成功。

### A6 后台命令

- 提示要求把 `sleep 20 && echo done-bg` 放到后台运行。
- 结果：
  - `run_command` 在 t=8.6s 时为 `ACTIVE`，**一直保持 ACTIVE**，到 28.7s 才变 DONE，output 为 `done-bg`。
  - stderr：`root agent idle; waiting up to 30m0s for 1 background task(s)`。
  - 随后出现 `system_message` 步，即完成通知，然后是最终 `agent_response`。`result` 在 31.8s 到达，exit 0。
  - 没有残留的 sleep 进程。
- **结论**：headless 模式**不会在后台命令结束前结束回合**，会等待，上限由 `--print-timeout` 控制（最长 30 分钟）。事件流里只能看到 tool 步长时间处于 ACTIVE，没有专门的后台事件，只有完成后的 `system_message`。
- **对 pactify 的影响**：pactify 传 `--print-timeout 0s`，agy 最多会等后台任务 30 分钟，这会计入任务时长；dev server 之类的 daemon 后台任务按 changelog 会被保留，没有实测。

---

## OPENCODE

### O1 serve 与认证

- 命令：`ocenv.sh opencode serve --port 47311 --hostname 127.0.0.1`，设置 `OPENCODE_SERVER_PASSWORD=probe-pw`。
- 结果：
  - 未认证访问 `/doc` 和 `/session` 返回 **401**。Basic `opencode:probe-pw` 返回 200。错误密码返回 401。
  - 用户名默认为 `opencode`，可用 `OPENCODE_SERVER_USERNAME` 修改。
  - 不设密码时，stdout 打印 `Warning: OPENCODE_SERVER_PASSWORD is not set; server is unsecured.`，未认证访问 `/doc` 和 `/session` 返回 **200**。
  - `/doc` 返回 OpenAPI 3.1，含两套 API：
    - v1：`/session`、`/session/{id}/message|prompt_async|abort`、`/permission/{id}/reply`、`/event`
    - v2：`/api/session…`、`/api/model`、`/api/session/{id}/event|interrupt|wait|permission`
- **结论：confirmed**。pactify 必须给每个 serve 生成随机密码。

### O2 plan agent

- **v2 路径**：`POST /api/session {agent:"plan",model:{id,providerID},location:{directory}}`，然后 `POST /api/session/{id}/prompt`。
  - prompt 返回 200 `admittedSeq:1`，SSE 只有 `session.next.prompt.admitted` 和 `session.next.prompted` 两个事件，**之后不再执行**。
  - 服务端日志：`Failed to drain Session … SessionRunnerModel.UnsupportedApiError: Unsupported API for opencode/deepseek-v4.1-flash-free: native`。
  - `/api/session/{id}/wait` 返回 503 `"Session wait is not available yet"`。
  - **v2 在 1.19.0 尚不可用，错误也不会通过 API 暴露。**
- **v1 路径**：`POST /session {title}`，然后同步调用 `POST /session/{id}/message {agent:"plan",model:{providerID:"opencode",modelID:"deepseek-v4.1-flash-free"},parts:[{type:"text",text:"Create a file x.txt containing hi… Also list the repository files with ls."}]}`。
  - 6.5s 返回 200。
  - 第一条 assistant 消息执行了 `bash {"command":"ls"}`，状态 completed，输出 `README.md`，没有触发权限请求。
  - 第二条消息 `finish:"stop"`，给出计划："I can proceed once you approve exiting plan mode"。
  - 仓库无变化，没有 error。
- **结论：confirmed**（v1）。plan agent 不写文件，不出错，能跑只读 bash，并给出结论。
- **注意**：同步 `/message` 只返回**最后一条** AssistantMessage 的 tokens；会话汇总在 `GET /session/{id}` 的 `tokens/cost`。

### O3 build agent、权限与 SSE

- 会话创建时带 `permission:[{permission:"edit",pattern:"*",action:"ask"},{permission:"bash",pattern:"*",action:"ask"}]`，然后调用 `prompt_async`（204）。驱动脚本：`o3.py`。
- 结果：
  - t=4.7s 收到 `permission.asked`，内容为 `{id:"per_…",permission:"edit",patterns:["hello.txt"],metadata:{filepath,diff},always:[…],tool:{messageID,callID}}`。
  - `GET /permission` 能列出该请求。**`GET /api/session/{id}/permission`（v2）返回空 `[]`**。
  - `POST /permission/{id}/reply {"reply":"once"}` 返回 200 true，随后依次出现 `permission.replied`、`file.edited`、tool completed。最终 `hello.txt` 内容为 `hi`。
- **事件类型统计**：`server.connected, session.created, session.updated×5, message.updated×10, message.part.updated×13, message.part.delta×36, session.status×6 (busy…→idle), session.diff, permission.asked, permission.replied, file.edited, file.watcher.updated, session.idle`。首次连接还会有 `plugin.added`、`catalog.updated`。
- **part 类型**：`text, step-start, reasoning, tool(pending→running→completed), step-finish{reason,tokens,cost}, patch{hash,files}`。
- **最终 AssistantMessage 字段**：
  ```
  {agent:"build",modelID:"deepseek-v4.1-flash-free",providerID:"opencode",finish:"stop"|"tool-calls",cost:0,tokens:{total,input,output,reasoning,cache:{read,write}},time:{created,completed},error:null,path:{cwd,root},mode,parentID}
  ```
- 会话汇总 tokens：`{input:2368,output:113,reasoning:111,cache:{read:14464,write:0}}`。
- **结论：confirmed**（v1 的 `/event` 和 `/permission`）。任务里提到的 `/api/session/{id}/event` 和 `/api/session/{id}/permission`（v2）对 v1 循环**无效**。

### O4 错误路径

- **O4a**：同步 `/message`，model 为 `opencode/no-such-model-free`。
  - HTTP **500**：`{"name":"UnknownError","data":{"message":"Unexpected server error…","ref":"err_544db2fb"}}`。
  - SSE：`session.error {name:"UnknownError",data:{message:"Model not found: opencode/no-such-model-free. Did you mean: …"}}`，随后 `session.idle`。
  - 不会生成 assistant 消息。
- **O4b**：`prompt_async`，provider 为 `nope-provider`。
  - HTTP **204**。错误只出现在 SSE 的 `session.error`（UnknownError "Model not found: nope-provider/x."，还有一条带堆栈的重复事件），消息列表里只有 user 消息。
- **O4c**：通过 `OPENCODE_CONFIG_CONTENT` 注入一个 `@ai-sdk/openai-compatible` 假 provider，指向本地假服务器。
  - 400：同步 `/message` 返回 HTTP **200**，`info.error` 为 `{"name":"APIError","data":{"message":"fake 400 from probe","statusCode":400,"isRetryable":false,"responseHeaders":{…},"responseBody":"…","metadata":{"url":…}}}`。SSE 同时有 `session.error` 和 assistant 的 `message.updated.error`，tokens 全为 0。
  - 429（带 retry-after:1）：`session.status {type:"retry",attempt:1..5,message,next}` 共 5 次重试，总计 6 次请求，然后 `APIError statusCode:429,isRetryable:true`，状态回到 idle。
- **结论：confirmed**。错误有三种表现：
  - 配置或模型错误：同步请求得到 HTTP 500 UnknownError，异步请求只有 SSE 事件。
  - provider HTTP 错误：HTTP 200，`message.error` 为 APIError（含 statusCode 和 isRetryable）。
  - 重试过程：通过 `session.status` 的 `retry` 可见。
- **对 pactify 的影响**：异步路径必须订阅 SSE 的 `session.error`，不能依赖 HTTP 状态码。

### O5 中断

- 驱动脚本：`o5.py`。bash 正在执行 `sleep 37 && echo finished-37`（进程 zsh 99351 / sleep 99352，同一 pgid）。
- `POST /api/session/{id}/interrupt`（v2）返回 204，但**是空操作**：3 秒后进程仍在，也没有事件。
- `POST /session/{id}/abort`（v1）返回 200 true，3 秒内进程全部消失。
- 事件依次为：
  - `session.error {name:"MessageAbortedError",data:{message:"Aborted"}}`
  - bash part `status:"completed"`，output 含 `User aborted the command`，`exit:null`
  - assistant `message.updated.error=MessageAbortedError`
  - `session.idle`
- **结论**：v2 interrupt 无效（refuted）；v1 abort 能杀掉子进程组（confirmed）。

### O6 重启后续接

- 杀掉 serve 后用相同的 XDG 重启。`GET /session/ses_ebb5ea53…` 返回 200，3 条旧消息都在。
- 向旧会话发 `POST /session/{id}/message` 返回 200，回答为 "hello.txt, containing exactly hi"，上下文保留。
- 状态持久化在 `XDG_DATA_HOME/opencode/opencode.db`（SQLite + WAL；表包括 session/message/part/permission/event…），快照在 `…/opencode/snapshot/`。
- **结论：confirmed**。pactify 的私有 XDG_DATA_HOME 即是续接所需的状态目录，不能在两次续接之间清理。

### O7 模型列表（不计费）

- `GET /api/model?location[directory]=…`（v2）：200，耗时 1.5s，返回 43 个模型，**全部属于 `opencode` provider 且 cost 为 0**：41 个 `*-free`，另有 `big-pickle` 和 `grok-code`。通过 auth.json 登录的 deepseek、google、minimax 等 provider **不在其中**。
  - 字段：`id, providerID, name, api{id,type:"native",url}, capabilities{tools,input,output}, cost[{input,output,cache}], status, enabled, limit{context,output}, time.released, request, variants`。
- `GET /config/providers?directory=…`（v1）：返回 `{providers:[{id,name,source,env,key,options,models{…}}], default:{opencode:"big-pickle",deepseek:"deepseek-v4-pro",…,"minimax-coding-plan":"MiniMax-M3.1-Flash-Preview"}}`。
  - provider：opencode(86)、deepseek(2)、google(39)、minimax-cn(7)、minimax-coding-plan(8)。
  - 注意：minimax-coding-plan 各模型的 cost 都标为 0，**不能用 cost==0 判断免费**。
- **结论：confirmed**。两个接口都不计费。选免费模型应按 `-free` 后缀，与 pactify 现有的 `opencodeDoctorModel` 一致。

## 汇总：对 pactify 的主要影响

1. agy 只读：`--mode plan` 加上私有 home 中 `settings.json` 的 `permissions.allow` 只读白名单，可以收敛。不加白名单则会以空回答结束；要检查 `denied_actions`。
2. agy 守卫：使用私有 `.gemini/config/hooks.json`。PreToolUse deny 不会中断回合；Stop 返回 continue 可以强制继续。不应依赖 `--sandbox`：它不封网络和端口，阻止工作区写入，而且在 skip-permissions 下会被 `BypassSandbox` 自动绕过。
3. agy 常驻：stream-json 多轮可行；`result.usage` 是累计值；坏输入行会导致进程退出；必须显式传 `--model`（默认是 Argon）；print-timeout 到期会被报告为 SUCCESS。
4. opencode serve：必须设 `OPENCODE_SERVER_PASSWORD`；只用 v1 API（v2 的 prompt、wait、interrupt 和权限列表在 1.19.0 不可用或为空操作）；异步错误只能从 SSE 得到；中断用 v1 `/abort`；续接依赖私有 XDG_DATA_HOME。
