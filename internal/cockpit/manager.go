package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SessionKey identifies a cockpit session: one per (project, seat).
type SessionKey struct{ Project, Seat string }

func (k SessionKey) String() string { return k.Project + "__" + k.Seat }

// BackendFactory returns the Backend to host a given key's session. Injected so
// tests use FakeBackend and D2-3 can select by seat kind (claude/codex/acp).
type BackendFactory func(key SessionKey) (Backend, error)

// AuditSink receives cockpit audit events bound to a specific (project, seat).
type AuditSink func(key SessionKey, ev AuditEvent)

// Manager owns the live cockpit sessions for a serve process.
//
// Pending approvals are bound to a live CockpitSession (and its backend
// subprocess). When serve restarts, every live session is torn down and the
// pending list is intentionally empty: the Respond closures point to dead
// processes and can never answer. This is the correct semantics; callers that
// need durable approval state must persist decisions via the pact log, not the
// cockpit runtime.
type Manager struct {
	baseDir string
	factory BackendFactory
	baseCtx context.Context
	sink    AuditSink

	mu           sync.Mutex
	sessions     map[SessionKey]*CockpitSession
	cancels      map[SessionKey]context.CancelFunc
	idleTimeout  time.Duration

	threads     map[string]string // SessionKey.String() -> threadID
	threadsOnce sync.Once
}

// ManagerOption configures a Manager on creation.
type ManagerOption func(*Manager)

// WithIdleTimeout overrides the default 30-minute idle timeout. Intended for
// tests; a value ≤ 0 keeps the default.
func WithIdleTimeout(d time.Duration) ManagerOption {
	return func(m *Manager) { m.idleTimeout = d }
}

func (m *Manager) threadsFile() string {
	return filepath.Join(m.baseDir, "cockpit", "threads.json")
}

func (m *Manager) ensureThreadsLoaded() {
	m.threadsOnce.Do(func() {
		m.threads = make(map[string]string)
		data, err := os.ReadFile(m.threadsFile())
		if err != nil {
			return // file may not exist yet
		}
		_ = json.Unmarshal(data, &m.threads)
	})
}

func (m *Manager) writeThreadsLocked() error {
	if err := os.MkdirAll(filepath.Dir(m.threadsFile()), 0o700); err != nil {
		return fmt.Errorf("mkdir threads dir: %w", err)
	}
	tmp := m.threadsFile() + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open threads tmp: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m.threads); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("encode threads: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close threads tmp: %w", err)
	}
	if err := os.Rename(tmp, m.threadsFile()); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename threads file: %w", err)
	}
	return nil
}

// NoteThread persists a non-empty threadID for key so a future serve process
// can resume the conversation. It is idempotent: storing the same threadID is
// a no-op, and an empty threadID is ignored.
func (m *Manager) NoteThread(key SessionKey, threadID string) {
	if threadID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureThreadsLoaded()
	if m.threads[key.String()] == threadID {
		return
	}
	m.threads[key.String()] = threadID
	_ = m.writeThreadsLocked() // best-effort persistence
}

// StoredThread returns the persisted threadID for key, if any.
func (m *Manager) StoredThread(key SessionKey) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureThreadsLoaded()
	return m.threads[key.String()]
}

// NewManager persists session event logs under baseDir/cockpit/. factory builds
// the backend per key. Sessions live under context.Background() (serve lifetime).
func NewManager(baseDir string, factory BackendFactory) *Manager {
	return NewManagerCtx(context.Background(), baseDir, factory)
}

// NewManagerCtx is NewManager with an explicit base context — when it is
// cancelled (serve shutdown), every session's backend process is torn down.
func NewManagerCtx(baseCtx context.Context, baseDir string, factory BackendFactory) *Manager {
	return NewManagerCtxAudit(baseCtx, baseDir, factory, nil)
}

// NewManagerCtxAudit is NewManagerCtx with an optional audit sink. The sink is
// invoked for every live CockpitSession under this manager.
func NewManagerCtxAudit(baseCtx context.Context, baseDir string, factory BackendFactory, sink AuditSink, opts ...ManagerOption) *Manager {
	m := &Manager{
		baseDir:  baseDir,
		factory:  factory,
		baseCtx:  baseCtx,
		sink:     sink,
		sessions: make(map[SessionKey]*CockpitSession),
		cancels:  make(map[SessionKey]context.CancelFunc),
		threads:  make(map[string]string),
	}
	for _, opt := range opts {
		opt(m)
	}
	if m.idleTimeout <= 0 {
		m.idleTimeout = 30 * time.Minute
	}
	go m.reaper(baseCtx)
	return m
}

// wrapSessionLocked creates a CockpitSession from an already-started Session and
// registers it under key. Caller must hold m.mu and supply a session-lifetime
// context and its cancel func.
func (m *Manager) wrapSessionLocked(sessCtx context.Context, key SessionKey, sess Session, cancel context.CancelFunc) (*CockpitSession, error) {
	jsonlPath := filepath.Join(m.baseDir, "cockpit", key.Project+"__"+key.Seat+".jsonl")
	var cs *CockpitSession
	var err error
	if m.sink != nil {
		cs, err = NewCockpitSessionWithAudit(sess, jsonlPath, func(ev AuditEvent) { m.sink(key, ev) })
	} else {
		cs, err = NewCockpitSession(sess, jsonlPath)
	}
	if err != nil {
		cancel()
		return nil, err
	}

	m.sessions[key] = cs
	m.cancels[key] = cancel
	return cs, nil
}

// Session returns the existing session for key, or starts one: factory(key) ->
// Backend.Start(sessionCtx, opts) -> wrapped in a CockpitSession with jsonl at
// baseDir/cockpit/<project>__<seat>.jsonl. Concurrent callers for the same key
// get the SAME session (single-flight under the manager lock).
//
// The backend process is spawned under a SESSION-lifetime context derived from
// the manager base context — NOT the caller's ctx. A cockpit session outlives
// the request that created it; binding the child process (exec.CommandContext)
// to an HTTP request context would kill the agent the moment POST /prompt
// returns. The caller's ctx is intentionally not used for the process lifetime;
// the backend handshakes (initialize/thread.start) are fast by design.
func (m *Manager) Session(_ context.Context, key SessionKey, opts StartOpts) (*CockpitSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cs, ok := m.sessions[key]; ok {
		cs.touch()
		return cs, nil
	}

	backend, err := m.factory(key)
	if err != nil {
		return nil, err
	}

	sessCtx, cancel := context.WithCancel(m.baseCtx)
	sess, err := backend.Start(sessCtx, opts)
	if err != nil {
		cancel()
		return nil, err
	}

	cs, err := m.wrapSessionLocked(sessCtx, key, sess, cancel)
	if err != nil {
		return nil, err
	}
	cs.touch()
	return cs, nil
}

// Resume returns the existing live session for key, or resumes a conversation
// from a persisted threadID: factory(key) -> Backend.Resume(sessCtx, threadID).
// The resumed session is wrapped and registered exactly like Session().
func (m *Manager) Resume(_ context.Context, key SessionKey, threadID string) (*CockpitSession, error) {
	if threadID == "" {
		return nil, fmt.Errorf("threadID is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if cs, ok := m.sessions[key]; ok {
		cs.touch()
		return cs, nil
	}

	backend, err := m.factory(key)
	if err != nil {
		return nil, err
	}

	sessCtx, cancel := context.WithCancel(m.baseCtx)
	sess, err := backend.Resume(sessCtx, threadID)
	if err != nil {
		cancel()
		return nil, err
	}

	cs, err := m.wrapSessionLocked(sessCtx, key, sess, cancel)
	if err != nil {
		return nil, err
	}
	cs.touch()
	return cs, nil
}

// Get returns the live session for key without creating one.
func (m *Manager) Get(key SessionKey) (*CockpitSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cs, ok := m.sessions[key]
	return cs, ok
}

// List returns the keys of all live sessions (for status).
func (m *Manager) List() []SessionKey {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]SessionKey, 0, len(m.sessions))
	for key := range m.sessions {
		out = append(out, key)
	}
	return out
}

// Close closes and removes one session (no-op if absent), tearing down its
// backend process (cancel the session context + CockpitSession.Close).
func (m *Manager) Close(key SessionKey) error {
	m.mu.Lock()
	cs, ok := m.sessions[key]
	if ok {
		delete(m.sessions, key)
		if cancel := m.cancels[key]; cancel != nil {
			cancel()
			delete(m.cancels, key)
		}
	}
	m.mu.Unlock()

	if !ok {
		return nil
	}
	return cs.Close()
}

// CloseAll tears down every session (serve Stop). Returns the first error.
func (m *Manager) CloseAll() error {
	m.mu.Lock()
	snapshot := make([]*CockpitSession, 0, len(m.sessions))
	for _, cs := range m.sessions {
		snapshot = append(snapshot, cs)
	}
	for _, cancel := range m.cancels {
		cancel()
	}
	m.sessions = make(map[SessionKey]*CockpitSession)
	m.cancels = make(map[SessionKey]context.CancelFunc)
	m.mu.Unlock()

	var firstErr error
	for _, cs := range snapshot {
		if err := cs.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// reaper scans for idle sessions and closes them. It exits when baseCtx is
// cancelled. The scan interval is deliberately coarse (1 minute) because this
// is a leak-guard, not a precision timer.
func (m *Manager) reaper(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.CloseIdleSessions()
		}
	}
}

// CloseIdleSessions closes every session whose last activity is older than the
// configured idleTimeout. It is safe to call concurrently and is exported so
// tests can reap on demand without waiting for the reaper ticker.
func (m *Manager) CloseIdleSessions() {
	m.mu.Lock()
	now := time.Now()
	type item struct {
		key SessionKey
		cs  *CockpitSession
	}
	var expired []item
	for key, cs := range m.sessions {
		if cs.idleSince(now) > m.idleTimeout {
			expired = append(expired, item{key: key, cs: cs})
			delete(m.sessions, key)
			if cancel := m.cancels[key]; cancel != nil {
				cancel()
				delete(m.cancels, key)
			}
		}
	}
	m.mu.Unlock()

	for _, it := range expired {
		_ = it.cs.Close()
	}
}
