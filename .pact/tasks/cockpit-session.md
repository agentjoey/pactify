# Task cockpit-session (D2-1a) — CockpitSession(单会话宿主核)

## 目标(orchestrator-cockpit-spec E1a 的最小承重件)
在 `internal/cockpit` 加 `CockpitSession`:托管一个 `Session`(A1 接口),提供 cockpit 需要的
多轮宿主能力——**串行 Prompt** + **事件落盘 JSONL + 实时 fan-out 给订阅者** + **pending 审批队列(稳定 id)+ Respond**。
后端无关,用 A1 的 `FakeSession` 可完整单测。纯新增。

## 改文件(仅新增)
- `internal/cockpit/session.go`
- `internal/cockpit/session_test.go`

## 契约(session.go)
```go
package cockpit

// PendingApproval is one unanswered approval, with a session-stable id the UI
// echoes back to Respond. RawInput is the full tool input (审批卡信任根).
type PendingApproval struct {
    ID       string          // manager-assigned, stable within the session
    Kind     string
    ToolName string
    RawInput json.RawMessage
}

// CockpitSession hosts one Backend Session: serial prompts, event persistence +
// live fan-out, and a pending-approval queue. Safe for concurrent callers.
type CockpitSession struct { ... }

// NewCockpitSession wraps sess, appends every event to jsonlPath (0o600, dir
// 0o700), and starts background pumps draining sess.Events()/sess.Approvals().
func NewCockpitSession(sess Session, jsonlPath string) (*CockpitSession, error)

func (cs *CockpitSession) Prompt(ctx context.Context, text string) error // 串行(mu 或队列)
func (cs *CockpitSession) Interrupt(ctx context.Context) error
func (cs *CockpitSession) ThreadID() string
func (cs *CockpitSession) Pending() []PendingApproval          // 未答审批快照(顺序稳定)
func (cs *CockpitSession) Respond(approvalID string, d Decision) error // 找到→Respond→出队;未知 id 返回 error
func (cs *CockpitSession) Subscribe() (int, <-chan Event)      // 实时事件 fan-out;返回订阅 id + channel
func (cs *CockpitSession) Unsubscribe(id int)
func (cs *CockpitSession) History() ([]Event, error)          // 读回落盘的 JSONL(重放)
func (cs *CockpitSession) Close() error                        // 幂等:停 pump、Unsubscribe 全部、sess.Close
```

## 行为要求
- **事件 pump**:一个 goroutine `for e := range sess.Events()`:①追加一行 JSON 到 jsonlPath
  (os.OpenFile append|create 0o600;目录 MkdirAll 0o700);②非阻塞 fan-out 给所有订阅者
  (每个订阅 channel 有缓冲,满则丢最旧或直接跳过——**绝不阻塞 pump**)。channel 关闭时 pump 退出。
- **审批 pump**:一个 goroutine `for a := range sess.Approvals()`:给每个分配稳定 id
  (自增计数,如 "ap1"、"ap2"),存进 pending 队列(map + 有序 slice),保留其 Respond 闭包。
- **Respond(id,d)**:查 pending,命中则调其 `Respond(d)` + 从队列移除;未知 id 返回 error;
  重复 Respond 同 id 返回 error(已出队)。
- **Prompt 串行**:同一时刻只一个 Prompt 进行中(mu 保护或串行队列);直接透传 sess.Prompt。
- **Subscribe/Unsubscribe**:线程安全;Close 时关闭所有订阅 channel。
- **Close 幂等**:sync.Once;停 pump(靠 sess.Events()/Approvals() 在 sess.Close 后关闭)、关订阅、sess.Close。
- 所有共享状态(pending、subscribers、prompt mu)用锁保护;pump 回调不要持锁写 channel 时死锁。

## 测试(session_test.go,用 A1 的 FakeSession)
- 建 FakeSession + NewCockpitSession(jsonlPath=t.TempDir());
- Subscribe → FakeSession.Emit 几个 Event → 订阅 channel 按序收到 + jsonlPath 落了对应行;History() 读回一致。
- FakeSession.EmitApproval → Pending() 出现一条带稳定 id → Respond(id, DecisionAllow) 成功、该 id 出队、
  底层 Respond 被调用(fake 记录);未知 id Respond 报错;重复 Respond 报错。
- Prompt 透传到 FakeSession.Prompts;ThreadID 透传。
- Close 幂等(调两次不 panic);Close 后订阅 channel 关闭。
- 并发:多个订阅者同时收、pump 不阻塞(可 -race 下多 Emit)。

## 验收 / Acceptance(视角: correctness — 落盘+fanout+审批队列正确、pump 不阻塞、Close 幂等、-race 干净)
- reviewer 独立跑 verify(含 -race)。

## verify
verify: go build ./... && go test -race ./internal/cockpit/
