package cockpit

import (
	"context"
	"errors"
	"sync"
)

// FakeBackend 是 Backend 的内存 fake，用于 manager 集成与单测。
type FakeBackend struct{}

// NewFakeBackend 返回一个可用的 FakeBackend。
func NewFakeBackend() *FakeBackend {
	return &FakeBackend{}
}

// Start 创建一个新的 FakeSession，ThreadID 使用 opts.Seat。
func (b *FakeBackend) Start(ctx context.Context, opts StartOpts) (Session, error) {
	return NewFakeSession(opts.Seat), nil
}

// Resume 按 threadID 恢复一个 FakeSession。
func (b *FakeBackend) Resume(ctx context.Context, threadID string) (Session, error) {
	return NewFakeSession(threadID), nil
}

// FakeSession 是 Session 的内存 fake，可被测试手动驱动。
type FakeSession struct {
	threadID string

	mu              sync.Mutex
	Prompts         []UserMessage
	InterruptCalled bool

	events    chan Event
	approvals chan ApprovalRequest
	closeOnce sync.Once
}

// NewFakeSession 创建带缓冲 channel 的 FakeSession。
func NewFakeSession(threadID string) *FakeSession {
	return &FakeSession{
		threadID:  threadID,
		events:    make(chan Event, 16),
		approvals: make(chan ApprovalRequest, 16),
	}
}

// ThreadID 返回会话持久句柄。
func (s *FakeSession) ThreadID() string {
	return s.threadID
}

// Prompt 记录用户消息并按串行语义立即返回。
func (s *FakeSession) Prompt(ctx context.Context, msg UserMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Prompts = append(s.Prompts, msg)
	return nil
}

// Interrupt 记录被调用。
func (s *FakeSession) Interrupt(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InterruptCalled = true
	return nil
}

// Events 返回统一信封流；会话结束时 channel 会被 close。
func (s *FakeSession) Events() <-chan Event {
	return s.events
}

// Approvals 返回审批请求流；会话结束时 channel 会被 close。
func (s *FakeSession) Approvals() <-chan ApprovalRequest {
	return s.approvals
}

// Close 幂等地关闭 Events 与 Approvals channel。
func (s *FakeSession) Close() error {
	s.closeOnce.Do(func() {
		close(s.events)
		close(s.approvals)
	})
	return nil
}

// Emit 把事件写入 Events() channel，供测试确定性驱动。
func (s *FakeSession) Emit(e Event) {
	s.events <- e
}

// EmitApproval 把审批请求写入 Approvals() channel；Respond 幂等。
func (s *FakeSession) EmitApproval(a ApprovalRequest) {
	inner := a.Respond
	var once sync.Once
	var innerErr error
	a.Respond = func(d Decision) error {
		triggered := false
		once.Do(func() {
			triggered = true
			if inner != nil {
				innerErr = inner(d)
			}
		})
		if !triggered {
			return errors.New("approval already responded")
		}
		return innerErr
	}
	s.approvals <- a
}
