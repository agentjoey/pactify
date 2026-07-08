package cockpit

import (
	"context"
	"path/filepath"
	"sync"
)

// SessionKey identifies a cockpit session: one per (project, seat).
type SessionKey struct{ Project, Seat string }

// BackendFactory returns the Backend to host a given key's session. Injected so
// tests use FakeBackend and D2-3 can select by seat kind (claude/codex/acp).
type BackendFactory func(key SessionKey) (Backend, error)

// Manager owns the live cockpit sessions for a serve process.
type Manager struct {
	baseDir string
	factory BackendFactory
	baseCtx context.Context

	mu       sync.Mutex
	sessions map[SessionKey]*CockpitSession
	cancels  map[SessionKey]context.CancelFunc
}

// NewManager persists session event logs under baseDir/cockpit/. factory builds
// the backend per key. Sessions live under context.Background() (serve lifetime).
func NewManager(baseDir string, factory BackendFactory) *Manager {
	return NewManagerCtx(context.Background(), baseDir, factory)
}

// NewManagerCtx is NewManager with an explicit base context — when it is
// cancelled (serve shutdown), every session's backend process is torn down.
func NewManagerCtx(baseCtx context.Context, baseDir string, factory BackendFactory) *Manager {
	return &Manager{
		baseDir:  baseDir,
		factory:  factory,
		baseCtx:  baseCtx,
		sessions: make(map[SessionKey]*CockpitSession),
		cancels:  make(map[SessionKey]context.CancelFunc),
	}
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

	jsonlPath := filepath.Join(m.baseDir, "cockpit", key.Project+"__"+key.Seat+".jsonl")
	cs, err := NewCockpitSession(sess, jsonlPath)
	if err != nil {
		cancel()
		return nil, err
	}

	m.sessions[key] = cs
	m.cancels[key] = cancel
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
