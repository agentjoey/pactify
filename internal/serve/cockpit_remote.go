package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentjoey/pactify/internal/cloudauth"
	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/remoteexec"
)

const cockpitMirrorTTL = 5 * time.Minute

// serveCockpiter implements remoteexec.Cockpiter by bridging to the local
// cockpit.Manager. It also owns the upward ephemeral event mirror created by
// cockpit.subscribe: each cockpit event is encrypted under the project's key
// and emitted as an "ingest" WireMessage on the machine socket.
type serveCockpiter struct {
	s         *Server
	mu        sync.Mutex
	mirrors   map[cockpit.SessionKey]*mirrorSub
	emit      func(agentKind string, msg any) error
	encrypt   func(project string, payload []byte) (cloudauth.EncryptedBlob, error)
	machineID string
}

func (s *Server) newCockpiter(machineID string) remoteexec.Cockpiter {
	emit := func(agentKind string, msg any) error {
		if fn := s.getCockpitEmitter(); fn != nil {
			return fn(agentKind, msg)
		}
		return fmt.Errorf("machine channel not connected")
	}
	encrypt := func(project string, payload []byte) (cloudauth.EncryptedBlob, error) {
		if s.relay == nil {
			return cloudauth.EncryptedBlob{}, fmt.Errorf("relay not configured")
		}
		key, err := s.relay.projectKey(project)
		if err != nil {
			return cloudauth.EncryptedBlob{}, err
		}
		return cloudauth.EncryptEvent(key, payload)
	}
	return newServeCockpiter(s, machineID, emit, encrypt)
}

func newServeCockpiter(
	s *Server,
	machineID string,
	emit func(agentKind string, msg any) error,
	encrypt func(project string, payload []byte) (cloudauth.EncryptedBlob, error),
) *serveCockpiter {
	return &serveCockpiter{
		s:         s,
		mirrors:   make(map[cockpit.SessionKey]*mirrorSub),
		emit:      emit,
		encrypt:   encrypt,
		machineID: machineID,
	}
}

// Prompt forwards a remote prompt to the target seat's cockpit session.
func (sc *serveCockpiter) Prompt(project, seat, text string) error {
	cs, _, err := sc.s.cockpitSessionFor(context.Background(), project, seat)
	if err != nil {
		return err
	}
	return cs.Prompt(context.Background(), text)
}

// Permission resolves a pending approval in the target seat's session.
func (sc *serveCockpiter) Permission(project, seat, requestID, decision string) error {
	cs, ok, err := sc.s.cockpitSessionGet(project, seat)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("session not found")
	}
	d, ok := cockpitDecision(decision)
	if !ok {
		return fmt.Errorf("invalid decision %q", decision)
	}
	if err := cs.Respond(requestID, d); err != nil {
		return err
	}
	sc.emitApprovalResolved(cockpit.SessionKey{Project: project, Seat: seat}, requestID, decision)
	return nil
}

// Cancel interrupts the target seat's current turn.
func (sc *serveCockpiter) Cancel(project, seat string) error {
	cs, ok, err := sc.s.cockpitSessionGet(project, seat)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("session not found")
	}
	return cs.Interrupt(context.Background())
}

// Resume restores the target seat's persisted thread.
func (sc *serveCockpiter) Resume(project, seat string) error {
	_, _, err := sc.s.cockpitResumeSession(context.Background(), project, seat)
	return err
}

// Subscribe opens (or renews) an ephemeral upward mirror of the seat's cockpit
// events. The mirror expires after cockpitMirrorTTL unless renewed by another
// subscribe call, and stops automatically when the session closes.
func (sc *serveCockpiter) Subscribe(project, seat string) error {
	cs, _, err := sc.s.cockpitSessionFor(context.Background(), project, seat)
	if err != nil {
		return err
	}
	key := cockpit.SessionKey{Project: project, Seat: seat}
	sc.mu.Lock()
	if sub, ok := sc.mirrors[key]; ok {
		if sub.renew() {
			sc.mu.Unlock()
			return nil
		}
		// TTL fired but stop() hasn't pruned the map yet — replace with a
		// fresh mirror below.
		delete(sc.mirrors, key)
	}
	subID, ch := cs.Subscribe()
	sub := &mirrorSub{
		key:   key,
		sc:    sc,
		cs:    cs,
		subID: subID,
		ch:    ch,
		ttl:   cockpitMirrorTTL,
		done:  make(chan struct{}),
	}
	sc.mirrors[key] = sub
	sc.mu.Unlock()
	sub.start()
	return nil
}

// mirrorSub holds one active remote subscription and its TTL timer.
type mirrorSub struct {
	key   cockpit.SessionKey
	sc    *serveCockpiter
	cs    *cockpit.CockpitSession
	subID int
	ch    <-chan cockpit.Event
	ttl   time.Duration

	mu             sync.Mutex
	done           chan struct{}
	closeOnce      sync.Once
	timer          *time.Timer
	lastPendingIDs []string
	seqVal         int64
}

// renew resets the TTL timer. Returns false if the mirror has already been
// closed (TTL fired / session gone) — the caller must create a fresh mirror.
func (m *mirrorSub) renew() bool {
	select {
	case <-m.done:
		return false
	default:
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timer != nil {
		m.timer.Stop()
	}
	m.timer = time.AfterFunc(m.ttl, func() { m.closeOnce.Do(func() { close(m.done) }) })
	return true
}

func (m *mirrorSub) stop() {
	m.mu.Lock()
	m.closeOnce.Do(func() { close(m.done) })
	if m.timer != nil {
		m.timer.Stop()
	}
	m.mu.Unlock()
	m.cs.Unsubscribe(m.subID)
	// mirrors is guarded by sc.mu, never m.mu (locks are taken separately, not
	// nested, so there is no order inversion with Subscribe's sc.mu → renew).
	// Only delete our own entry — a fresh mirror may already have replaced it.
	m.sc.mu.Lock()
	if m.sc.mirrors[m.key] == m {
		delete(m.sc.mirrors, m.key)
	}
	m.sc.mu.Unlock()
}

func (m *mirrorSub) start() {
	m.renew()
	go m.run()
}

func (m *mirrorSub) nextSeq() int64 {
	return atomic.AddInt64(&m.seqVal, 1)
}

func (m *mirrorSub) run() {
	defer m.stop()

	// First event is always a status snapshot so the remote client sees the
	// pending list + resumable flag + threadId immediately.
	m.sc.emitSnapshot(m.key, m.cs, m.nextSeq())

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	tokensIn := 0
	tokensOut := 0
	costMicros := int64(0)

	for {
		select {
		case e, ok := <-m.ch:
			if !ok {
				return
			}
			if e.Kind == cockpit.EventUsage && e.Usage != nil {
				tokensIn += e.Usage.InputTokens
				tokensOut += e.Usage.OutputTokens
				costMicros += int64(e.Usage.CostUSD * 1e6)
			}
			state, eventKind := mapCockpitEvent(e)
			m.sc.emitMirrorEvent(m.key, m.cs, m.nextSeq(), eventKind, state, e, tokensIn, tokensOut, costMicros)

		case <-ticker.C:
			m.sc.emitPendingDelta(m.key, m.cs, m.nextSeq(), &m.lastPendingIDs)

		case <-m.done:
			return
		}
	}
}

// wireHeader is the Go shape of cloud/wire OperationalHeader. Only the fields
// needed for the ephemeral cockpit mirror are populated.
type wireHeader struct {
	V                int    `json:"v"`
	MachineID        string `json:"machineId"`
	RunID            string `json:"runId"`
	Seq              int64  `json:"seq"`
	TS               int64  `json:"ts"`
	State            string `json:"state"`
	EventKind        string `json:"eventKind"`
	PendingApprovals int    `json:"pendingApprovals"`
	TokensIn         int    `json:"tokensIn"`
	TokensOut        int    `json:"tokensOut"`
	CostMicros       int64  `json:"costMicros,omitempty"`
	Ephemeral        bool   `json:"ephemeral,omitempty"`
}

type mirrorStatusBody struct {
	Kind      string               `json:"kind"`
	ThreadID  string               `json:"threadId"`
	Resumable bool                 `json:"resumable"`
	Pending   []cockpitPendingItem `json:"pending"`
}

func (sc *serveCockpiter) emitSnapshot(key cockpit.SessionKey, cs *cockpit.CockpitSession, seq int64) {
	body := mirrorStatusBody{
		Kind:      "status",
		ThreadID:  cs.ThreadID(),
		Resumable: sc.s.cockpit.StoredThread(key) != "",
		Pending:   pendingItemsFor(cs),
	}
	sc.emitMirrorEvent(key, cs, seq, "snapshot", "idle", body, 0, 0, 0)
}

func (sc *serveCockpiter) emitCockpitEvent(key cockpit.SessionKey, cs *cockpit.CockpitSession, e cockpit.Event, seq int64, tokensIn, tokensOut int, costMicros int64) {
	state, eventKind := mapCockpitEvent(e)
	sc.emitMirrorEvent(key, cs, seq, eventKind, state, e, tokensIn, tokensOut, costMicros)
}

func (sc *serveCockpiter) emitPendingDelta(key cockpit.SessionKey, cs *cockpit.CockpitSession, seq int64, lastPending *[]string) bool {
	pending := cs.Pending()
	ids := make([]string, len(pending))
	for i, p := range pending {
		ids[i] = p.ID
	}

	changed := len(ids) != len(*lastPending)
	if !changed {
		for i, id := range ids {
			if id != (*lastPending)[i] {
				changed = true
				break
			}
		}
	}
	if !changed {
		return false
	}
	*lastPending = ids

	body := map[string]any{
		"kind":    "approval-request",
		"pending": pendingItemsFor(cs),
	}
	sc.emitMirrorEvent(key, cs, seq, "approval-request", "awaiting-approval", body, 0, 0, 0)
	return true
}

func (sc *serveCockpiter) emitApprovalResolved(key cockpit.SessionKey, requestID, decision string) {
	sc.mu.Lock()
	sub, ok := sc.mirrors[key]
	sc.mu.Unlock()
	if !ok {
		return
	}
	body := map[string]any{
		"kind":      "approval-resolved",
		"requestId": requestID,
		"decision":  decision,
	}
	sc.emitMirrorEvent(key, sub.cs, sub.nextSeq(), "approval-resolved", "idle", body, 0, 0, 0)
}

func (sc *serveCockpiter) emitMirrorEvent(key cockpit.SessionKey, cs *cockpit.CockpitSession, seq int64, eventKind, state string, body any, tokensIn, tokensOut int, costMicros int64) {
	if sc.emit == nil || sc.encrypt == nil {
		return
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return
	}
	blob, err := sc.encrypt(key.Project, bodyJSON)
	if err != nil {
		return
	}
	header := wireHeader{
		V:                1,
		MachineID:        sc.machineID,
		RunID:            fmt.Sprintf("cockpit:%s:%s", key.Project, key.Seat),
		Seq:              seq,
		TS:               time.Now().UnixMilli(),
		State:            state,
		EventKind:        eventKind,
		PendingApprovals: len(cs.Pending()),
		TokensIn:         tokensIn,
		TokensOut:        tokensOut,
		CostMicros:       costMicros,
		Ephemeral:        true,
	}
	msg := map[string]any{
		"header": header,
		"body":   blob,
	}
	_ = sc.emit(agentKindForSeat(sc.s, key.Project, key.Seat), msg)
}

func mapCockpitEvent(e cockpit.Event) (state, eventKind string) {
	switch e.Kind {
	case cockpit.EventState:
		switch e.State {
		case "turn_completed":
			return "idle", "delta"
		case "turn_failed":
			// "error" is a RunState but NOT a wire EventKind — the relay
			// strict-rejects unknown kinds, silently dropping the message.
			return "error", "delta"
		default:
			return "thinking", "delta"
		}
	case cockpit.EventError:
		return "error", "delta"
	case cockpit.EventMessage:
		return "thinking", "message"
	case cockpit.EventUsage:
		return "thinking", "delta"
	default:
		return "thinking", "delta"
	}
}

func agentKindForSeat(s *Server, project, seat string) string {
	kind := s.seatKind(project, seat)
	if w, ok := pactToWireKind[kind]; ok {
		return w
	}
	return "claude"
}
