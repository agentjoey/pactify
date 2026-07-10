# Task cockpit-resume (E1b-b) — 跨重启会话恢复 + pending 审批契约

## 背景与契约(先定死语义)
serve 重启(launchd kickstart 是日常)= cockpit 后端子进程全灭:
- **pending 审批的契约 = 随进程消亡**(Respond 闭包指向死进程,不可能答)——重启后 status 的
  pending 必为空,这是正确语义,文档化即可(Manager 注释 + spec 引用)。
- **会话本身可恢复**:三档后端都支持 Resume(claude resume/codex thread/resume/ACP loadSession),
  但 threadId 现在只在内存——重启即失。本任务补 threadId 持久化 + 恢复链路。

## 改动
### internal/cockpit/manager.go
- threadId 持久化:`baseDir/cockpit/threads.json`(`map[string]string`,key "<project>__<seat>")。
  - 写:Session()/Resume 建会话后**及 cockpit/session 事件更新时**……简化:Manager 不订阅事件,
    改为**惰性写**——新增 `func (m *Manager) NoteThread(key SessionKey, threadID string)`(持久化,
    幂等,threadID 空忽略),由 serve 层在 status/prompt 拿到非空 ThreadID 时调用(见 serve 节)。
  - 读:`func (m *Manager) StoredThread(key SessionKey) string`。
  - 文件读写带简单锁(m.mu 即可),原子写(tmp+rename),0o600。
- `func (m *Manager) Resume(ctx, key, threadID string) (*CockpitSession, error)`:已有 live session
  直接返回;否则 factory→backend.Resume(sessCtx, threadID)→包 CockpitSession(同 Session(),含 audit
  sink/cancel 登记)。**复用 Session() 的建会话代码**(抽私有 helper,别复制两份)。
- Manager 注释写明 pending 契约(随进程消亡)。

### internal/serve/cockpit.go
- status:`cockpitStatusDTO` 加 `Resumable bool \`json:"resumable"\``——无 live session 且
  `StoredThread(key) != ""` 时 true;有 live session 时顺手 `NoteThread(key, cs.ThreadID())`。
- prompt handler:建会话后若 ThreadID 非空也 NoteThread(claude 的 threadId 是异步来的,空就跳过,
  status 轮询会补上)。
- 新端点 `POST /api/projects/{id}/cockpit/resume` body {seat}:StoredThread 为空→400
  "nothing to resume";否则 Manager.Resume→200 {ok,threadId}(错误→502 透传 error 字段)。

### web(api.ts + datasource + CockpitPanel)
- api.cockpitResume(project,seat);DataSource 可选方法;status 类型加 resumable。
- panel:capable && resumable && 无实时流内容时,顶部显示一条 "Previous session available —
  **Resume**" 按钮(data-testid="cockpit-resume");点击→cockpitResume→重连 stream+拉 status。

## 测试
- manager:NoteThread/StoredThread 持久化(重建 Manager 后仍读到);Resume 用 FakeBackend
  (记录收到的 threadID)断言传对 + 二次 Resume 返回同 live session。
- serve:无 session+有 stored → status.resumable=true;POST resume → FakeBackend.Resume 被调、
  200 带 threadId;无 stored → 400。
- web:resumable=true 渲染 Resume 按钮,点击调 cockpitResume。

## 验收(视角: correctness — 契约明确、threadId 幂等持久、Resume 三层贯通、-race)
verify: cd web && npx tsc --noEmit && npx vitest run src/components/CockpitPanel.test.tsx && cd .. && go test -race ./internal/cockpit/ ./internal/serve/
