package cockpit

import (
	"context"
	"errors"
	"sync"
)

// FakeBackend is an in-memory fake of Backend, used for manager integration and unit tests.
type FakeBackend struct{}

// NewFakeBackend returns a usable FakeBackend.
func NewFakeBackend() *FakeBackend {
	return &FakeBackend{}
}

// Start creates a new FakeSession, using opts.Seat as ThreadID.
func (b *FakeBackend) Start(ctx context.Context, opts StartOpts) (Session, error) {
	return NewFakeSession(opts.Seat), nil
}

// Resume restores a FakeSession by threadID.
func (b *FakeBackend) Resume(ctx context.Context, threadID string) (Session, error) {
	return NewFakeSession(threadID), nil
}

// FakeSession is an in-memory fake of Session that tests can drive manually.
type FakeSession struct {
	threadID string

	mu              sync.Mutex
	Prompts         []UserMessage
	InterruptCalled bool

	events    chan Event
	approvals chan ApprovalRequest
	closeOnce sync.Once
}

// NewFakeSession creates a FakeSession with buffered channels.
func NewFakeSession(threadID string) *FakeSession {
	return &FakeSession{
		threadID:  threadID,
		events:    make(chan Event, 16),
		approvals: make(chan ApprovalRequest, 16),
	}
}

// ThreadID returns the session's persistent handle.
func (s *FakeSession) ThreadID() string {
	return s.threadID
}

// Prompt records the user message and returns immediately with serialized semantics.
func (s *FakeSession) Prompt(ctx context.Context, msg UserMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Prompts = append(s.Prompts, msg)
	return nil
}

// Interrupt records that it was called.
func (s *FakeSession) Interrupt(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InterruptCalled = true
	return nil
}

// Events returns the unified envelope stream; the channel is closed when the session ends.
func (s *FakeSession) Events() <-chan Event {
	return s.events
}

// Approvals returns the approval request stream; the channel is closed when the session ends.
func (s *FakeSession) Approvals() <-chan ApprovalRequest {
	return s.approvals
}

// Close closes the Events and Approvals channels idempotently.
func (s *FakeSession) Close() error {
	s.closeOnce.Do(func() {
		close(s.events)
		close(s.approvals)
	})
	return nil
}

// Emit writes an event to the Events() channel for deterministic test driving.
func (s *FakeSession) Emit(e Event) {
	s.events <- e
}

// EmitApproval writes an approval request to the Approvals() channel; Respond is idempotent.
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
