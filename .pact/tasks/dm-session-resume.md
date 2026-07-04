# dm-session-resume — ACP session 续接(resume-instead-of-restart)

> 母 spec:`docs/specs/driver-modernization.md` §2。依赖 dm-acp-runner。

## 交付
1. `internal/orchestrate/sessionstore.go`:`.pact/orchestrate/sessions.json` 读写 `{seat, task, kind, sessionID, updatedAt}` 列表;文件锁用 `internal/lockx`(参考 merge 锁用法);原子写(tmp+rename)。
2. AcpRunner 集成:NewSession 成功 → 记录;重试同 (seat,task) → 若有记录且 server 能力支持 loadSession → `LoadSession` + briefing 前缀「续接上次会话:先自查上次进度(git log/diff),再继续任务」;Load 失败(过期/不支持)→ 删记录,静默 NewSession(=现状)。
3. 清理:任务 accepted/cancelled 时删除该 task 的记录(挂进 loop.go 现有 cleanupTaskSessions 附近);orchestrate 启动时扫一遍,删任务已终态的孤儿记录。

## 测试
假 server 声明/不声明 loadSession 两分支;重试路径断言 Load 先行、失败回退 New;终态清理;并发写锁(两 goroutine 同写不丢)。`go test ./internal/orchestrate/ -run 'Session'` 绿。

## 边界
CmdRunner/CLI 路径的 resume 不做(明确 out of scope);不动 sessions 包(那是 vendor session 清理,互补不冲突)。
