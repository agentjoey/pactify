# Task codex-exec-resume (W0c) — codex worker 重试续接(省 token)

## 事实(orchestrator 真机验证)
- `codex exec --json` 首行发 `{"type":"thread.started","thread_id":"<uuid>"}`(W0a 已加 --json)。
- `codex exec resume [SESSION_ID] [PROMPT]` 存在,接受 exec 的选项集(实现时读
  `codex exec resume --help` 全文核对 --json/--sandbox/-m 是否同样接受;若 resume 不接受某 flag,
  按 help 实况调整 resume 分支的参数)。
- 会话存取:`internal/orchestrate/sessionstore.go` 已有 `RecordSession(repoDir, seat, task, kind,
  sessionID)` + `LoadSessions(repoDir)`(lockx 保护)——ACP 路径已用它,codex 复用同一 store。

## 目标
codex 座席的 worker 棒重试(rework/fix 轮,同 seat+task 再跑)时,不再冷启动整个上下文,
改 `codex exec resume <thread_id> ...` 续接——对齐 ACP 档 loadSession 行为,省 token。

## 改动(internal/orchestrate,cmd 传输路径)
1. **捕获 thread_id**:CmdRunner 已 tee stdout(tokenCapture)。在 codex 棒结束后,从捕获的 JSONL
   里解析 `thread.started.thread_id`(加一个纯函数 `parseCodexThreadID(output string) string`,
   扫行找 type=="thread.started");非空则 `RecordSession(lc.RepoDir, lc.Seat, lc.Task, "codex-cli",
   threadID)`。挂点:CmdRunner.Run 里已有拿 capture 内容记 tokens 的地方,同处顺手做。
2. **重试时续接**:CmdRunner.Run 组装 args 前查 `LoadSessions(lc.RepoDir)` 找 (seat,task,kind=codex-cli)
   的记录:命中且 kind 是 codex-cli → args 改为 resume 形态:`exec resume <id> --json --sandbox
   <同原 posture> <briefing>`(以 help 实况为准;-m 同原逻辑)。未命中/其它 kind → 原样。
   抽 `codexResumeArgs(base []string, threadID string) []string` 或等价可测纯函数。
3. **失效兜底**:resume 退出非零时**不重试 resume**——但注意 CmdRunner 不知道失败原因;
   简化且安全的做法:resume 失败该棒记失败(现有失败处理),**并删除该 (seat,task) 的会话记录**
   (sessionstore 若无删除函数则加 `RemoveSession(repoDir, seat, task, kind)`),下一轮自然冷启动。
4. 只动 codex-cli;claude(--no-session-persistence 故意无会话)/其它 kind 零变化。

## 测试
- parseCodexThreadID:真实 JSONL 行→id;无 thread.started→""。
- codexResumeArgs:基础 args + id → resume 形态正确。
- CmdRunner 集成(用现有 fake execFn 模式):首跑记录 session;二跑同 (seat,task) args 含
  "resume <id>";resume 失败后记录被删、三跑回冷启动 args。
- 真机(reviewer 亲跑):小任务 codex 首跑捕获 thread_id 落 sessions.json;手动二跑续接可答上文。

## 验收(视角: correctness — 只影响 codex 重试路径、失效自愈、-race)
verify: go build ./... && go test -race ./internal/orchestrate/
