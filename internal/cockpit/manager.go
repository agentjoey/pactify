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

	mu       sync.Mutex
	sessions map[SessionKey]*CockpitSession
}

// NewManager persists session event logs under baseDir/cockpit/. factory builds
// the backend per key.
func NewManager(baseDir string, factory BackendFactory) *Manager {
	return &Manager{
		baseDir:  baseDir,
		factory:  factory,
		sessions: make(map[SessionKey]*CockpitSession),
	}
}

// Session returns the existing session for key, or starts one: factory(key) ->
// Backend.Start(ctx, opts) -> wrapped in a CockpitSession with jsonl at
// baseDir/cockpit/<project>__<seat>.jsonl. Concurrent callers for the same key
// get the SAME session (single-flight under the manager lock).
func (m *Manager) Session(ctx context.Context, key SessionKey, opts StartOpts) (*CockpitSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cs, ok := m.sessions[key]; ok {
		return cs, nil
	}

	backend, err := m.factory(key)
	if err != nil {
		return nil, err
	}

	sess, err := backend.Start(ctx, opts)
	if err != nil {
		return nil, err
	}

	jsonlPath := filepath.Join(m.baseDir, "cockpit", key.Project+"__"+key.Seat+".jsonl")
	cs, err := NewCockpitSession(sess, jsonlPath)
	if err != nil {
		return nil, err
	}

	m.sessions[key] = cs
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

// Close closes and removes one session (no-op if absent).
func (m *Manager) Close(key SessionKey) error {
	m.mu.Lock()
	cs, ok := m.sessions[key]
	if ok {
		delete(m.sessions, key)
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
	m.sessions = make(map[SessionKey]*CockpitSession)
	m.mu.Unlock()

	var firstErr error
	for _, cs := range snapshot {
		if err := cs.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
