package cockpit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// PendingApproval is one unanswered approval, with a session-stable id the UI
// echoes back to Respond. RawInput is the full tool input (审批卡信任根).
type PendingApproval struct {
	ID       string // manager-assigned, stable within the session
	Kind     string
	ToolName string
	RawInput json.RawMessage
}

// pendingEntry keeps an approval request plus its session-stable id.
type pendingEntry struct {
	id      string
	respond func(Decision) error
	pending PendingApproval
}

// subscriber holds a buffered fan-out channel for a single consumer.
type subscriber struct {
	id int
	ch chan Event
}

// CockpitSession hosts one Backend Session: serial prompts, event persistence +
// live fan-out, and a pending-approval queue. Safe for concurrent callers.
type CockpitSession struct {
	sess Session

	jsonlPath string
	fileMu    sync.Mutex

	mu           sync.Mutex
	promptMu     sync.Mutex
	pending      map[string]*pendingEntry
	pendingOrder []string
	nextApproval int
	subscribers  map[int]*subscriber
	nextSubID    int
	closed       bool

	closeOnce sync.Once
	closeErr  error
	pumpsWG   sync.WaitGroup
}

// NewCockpitSession wraps sess, appends every event to jsonlPath (0o600, dir
// 0o700), and starts background pumps draining sess.Events()/sess.Approvals().
func NewCockpitSession(sess Session, jsonlPath string) (*CockpitSession, error) {
	if err := os.MkdirAll(filepath.Dir(jsonlPath), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir jsonl dir: %w", err)
	}
	f, err := os.OpenFile(jsonlPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open jsonl: %w", err)
	}
	_ = f.Close()

	cs := &CockpitSession{
		sess:        sess,
		jsonlPath:   jsonlPath,
		pending:     make(map[string]*pendingEntry),
		subscribers: make(map[int]*subscriber),
	}

	cs.pumpsWG.Add(2)
	go cs.eventPump()
	go cs.approvalPump()
	return cs, nil
}

// Prompt forwards a user text prompt to the underlying session serially.
func (cs *CockpitSession) Prompt(ctx context.Context, text string) error {
	cs.promptMu.Lock()
	defer cs.promptMu.Unlock()
	return cs.sess.Prompt(ctx, UserMessage{Text: text})
}

// Interrupt forwards an interrupt request to the underlying session.
func (cs *CockpitSession) Interrupt(ctx context.Context) error {
	return cs.sess.Interrupt(ctx)
}

// ThreadID returns the underlying session's persistent thread handle.
func (cs *CockpitSession) ThreadID() string {
	return cs.sess.ThreadID()
}

// Pending returns an ordered snapshot of unanswered approvals.
func (cs *CockpitSession) Pending() []PendingApproval {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	out := make([]PendingApproval, 0, len(cs.pendingOrder))
	for _, id := range cs.pendingOrder {
		if e, ok := cs.pending[id]; ok {
			out = append(out, e.pending)
		}
	}
	return out
}

// Respond finds the pending approval by id, applies the decision, and removes it
// from the queue. Unknown or already-answered ids return an error.
func (cs *CockpitSession) Respond(approvalID string, d Decision) error {
	cs.mu.Lock()
	entry, ok := cs.pending[approvalID]
	if !ok {
		cs.mu.Unlock()
		return fmt.Errorf("approval %q not found", approvalID)
	}
	respond := entry.respond
	delete(cs.pending, approvalID)
	cs.removeFromPendingOrder(approvalID)
	cs.mu.Unlock()

	if err := respond(d); err != nil {
		return err
	}
	return nil
}

func (cs *CockpitSession) removeFromPendingOrder(id string) {
	for i, existing := range cs.pendingOrder {
		if existing == id {
			cs.pendingOrder = append(cs.pendingOrder[:i], cs.pendingOrder[i+1:]...)
			return
		}
	}
}

// Subscribe registers a new real-time event consumer and returns its id plus a
// receive-only channel. Each subscriber gets a buffered channel; events are
// dropped when the buffer is full so the pump never blocks.
func (cs *CockpitSession) Subscribe() (int, <-chan Event) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	id := cs.nextSubID
	cs.nextSubID++
	ch := make(chan Event, 64)
	sub := &subscriber{id: id, ch: ch}
	cs.subscribers[id] = sub
	return id, ch
}

// Unsubscribe removes a subscriber. Its channel is left open and will be
// closed when CockpitSession.Close runs.
func (cs *CockpitSession) Unsubscribe(id int) {
	cs.mu.Lock()
	delete(cs.subscribers, id)
	cs.mu.Unlock()
}

// History reads back all persisted events from the JSONL file.
func (cs *CockpitSession) History() ([]Event, error) {
	cs.fileMu.Lock()
	defer cs.fileMu.Unlock()

	f, err := os.Open(cs.jsonlPath)
	if err != nil {
		return nil, fmt.Errorf("open jsonl: %w", err)
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("unmarshal event: %w", err)
		}
		events = append(events, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan jsonl: %w", err)
	}
	return events, nil
}

// Close shuts down the session idempotently: it closes the underlying session,
// waits for the pumps to drain, then closes all subscriber channels.
func (cs *CockpitSession) Close() error {
	cs.closeOnce.Do(func() {
		cs.closeErr = cs.close()
	})
	return cs.closeErr
}

func (cs *CockpitSession) close() error {
	// Close the underlying session (which closes its Events/Approvals channels so
	// the pumps drain and exit), then reclaim regardless of its error — skipping
	// cleanup on a Close error would leak the pump goroutines and leave subscriber
	// channels open. The Close error is still surfaced.
	sessErr := cs.sess.Close()
	cs.pumpsWG.Wait()

	cs.mu.Lock()
	subs := make([]*subscriber, 0, len(cs.subscribers))
	for _, sub := range cs.subscribers {
		subs = append(subs, sub)
	}
	cs.subscribers = make(map[int]*subscriber)
	cs.closed = true
	cs.mu.Unlock()

	for _, sub := range subs {
		close(sub.ch)
	}
	return sessErr
}

// eventPump drains sess.Events(), persists each event to JSONL, and fans out to
// subscribers without blocking.
func (cs *CockpitSession) eventPump() {
	defer cs.pumpsWG.Done()
	for e := range cs.sess.Events() {
		if err := cs.appendEvent(e); err != nil {
			// Persistence failure is swallowed to keep the pump alive; consumers
			// can observe the event via fan-out.
			_ = err
		}
		cs.fanOut(e)
	}
}

func (cs *CockpitSession) appendEvent(e Event) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	cs.fileMu.Lock()
	defer cs.fileMu.Unlock()

	f, err := os.OpenFile(cs.jsonlPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(line)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func (cs *CockpitSession) fanOut(e Event) {
	cs.mu.Lock()
	subs := make([]*subscriber, 0, len(cs.subscribers))
	for _, sub := range cs.subscribers {
		subs = append(subs, sub)
	}
	cs.mu.Unlock()

	for _, sub := range subs {
		select {
		case sub.ch <- e:
		default:
			// Drop the event for this subscriber rather than block the pump.
		}
	}
}

// approvalPump drains sess.Approvals() and assigns each request a stable id.
func (cs *CockpitSession) approvalPump() {
	defer cs.pumpsWG.Done()
	for a := range cs.sess.Approvals() {
		cs.mu.Lock()
		cs.nextApproval++
		id := fmt.Sprintf("ap%d", cs.nextApproval)
		entry := &pendingEntry{
			id:      id,
			respond: a.Respond,
			pending: PendingApproval{
				ID:       id,
				Kind:     a.Kind,
				ToolName: a.ToolName,
				RawInput: a.RawInput,
			},
		}
		cs.pending[id] = entry
		cs.pendingOrder = append(cs.pendingOrder, id)
		cs.mu.Unlock()
	}
}
