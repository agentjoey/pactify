# Task cockpit-manager (D2-1b) — CockpitManager(per-(project,seat) 会话注册表)

## 目标(orchestrator-cockpit-spec E1a:serve 的子进程生命周期管理器)
在 `internal/cockpit` 加 `Manager`:按 (project,seat) 托管**至多一个** `CockpitSession`;
用可注入的 backend factory 造后端(测试用 FakeBackend,真实按 kind 选后端留 D2-3);
get-or-create + 单会话关闭 + 全体 teardown。后端无关,FakeBackend 可完整单测。纯新增。

## 改文件(仅新增)
- `internal/cockpit/manager.go`
- `internal/cockpit/manager_test.go`

## 契约(manager.go)
```go
package cockpit

// SessionKey identifies a cockpit session: one per (project, seat).
type SessionKey struct { Project, Seat string }

// BackendFactory returns the Backend to host a given key's session. Injected so
// tests use FakeBackend and D2-3 can select by seat kind (claude/codex/acp).
type BackendFactory func(key SessionKey) (Backend, error)

// Manager owns the live cockpit sessions for a serve process.
type Manager struct { ... }

// NewManager persists session event logs under baseDir/cockpit/. factory builds
// the backend per key.
func NewManager(baseDir string, factory BackendFactory) *Manager

// Session returns the existing session for key, or starts one: factory(key) →
// Backend.Start(ctx, opts) → wrapped in a CockpitSession with jsonl at
// baseDir/cockpit/<project>__<seat>.jsonl. Concurrent callers for the same key
// get the SAME session (single-flight under the manager lock).
func (m *Manager) Session(ctx context.Context, key SessionKey, opts StartOpts) (*CockpitSession, error)

// Get returns the live session for key without creating one.
func (m *Manager) Get(key SessionKey) (*CockpitSession, bool)

// List returns the keys of all live sessions (for status).
func (m *Manager) List() []SessionKey

// Close closes and removes one session (no-op if absent).
func (m *Manager) Close(key SessionKey) error

// CloseAll tears down every session (serve Stop). Returns the first error.
func (m *Manager) CloseAll() error
```

## 行为要求
- **单例 per key**:Session 在 manager 锁下检查 map;已存在则直接返回(不重启)。不存在则
  factory→Start→NewCockpitSession→存 map→返回。**注意**:Start 可能耗时(spawn 子进程),
  持整锁调用可接受(E1a MVP;后续可优化 single-flight),但**至少保证同 key 不并发建两个**。
  简单正确做法:持锁做全过程(建会话期间其它同 key 调用等待)。
- jsonlPath:`filepath.Join(baseDir, "cockpit", project+"__"+seat+".jsonl")`(seat/project 已是 slug,
  用 `__` 分隔避免撞 `-`)。
- factory 返回 error 或 Start error → Session 返回该 error,不存 map。
- Close(key):从 map 取出→删→调 CockpitSession.Close();不存在则 nil。
- CloseAll:快照所有会话→逐个 Close→返回第一个错误;清空 map。
- List:锁下拷贝 keys。
- 线程安全:一把 manager 锁保护 map。CockpitSession 自身已线程安全。

## 测试(manager_test.go,FakeBackend)
- factory 返回 FakeBackend;NewManager(t.TempDir(), factory)。
- Session(key1) 建会话,再 Session(key1) 返回**同一**指针(get-or-create);Session(key2) 是不同会话。
- 通过返回的 CockpitSession 能 Prompt/Subscribe(证明真接上了 FakeSession)。
- List() 返回两个 key;Get(key1) 命中、Get(未知) miss。
- Close(key1) 后 Get(key1) miss、List 只剩 key2;CloseAll 后 List 空。
- factory 返回 error 时 Session 返回 error 且 map 不留条目。
- 并发:多 goroutine 同时 Session(同 key) 只建一个(用一个计数 factory 断言只被调一次)——-race。
- jsonl 路径落在 baseDir/cockpit/ 下(可 Emit 后检查文件存在)。

## 验收 / Acceptance(视角: correctness — 单例 per key、并发只建一个、teardown 干净、-race)
- reviewer 独立跑 verify(含 -race)。

## verify
verify: go build ./... && go test -race ./internal/cockpit/
