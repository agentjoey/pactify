package cockpit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestFactory(t *testing.T, count *atomic.Int32) BackendFactory {
	return func(key SessionKey) (Backend, error) {
		if count != nil {
			count.Add(1)
		}
		return NewFakeBackend(), nil
	}
}

func TestManager_Session_getOrCreate(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	mgr := NewManager(baseDir, newTestFactory(t, nil))

	key1 := SessionKey{Project: "p1", Seat: "s1"}
	key2 := SessionKey{Project: "p1", Seat: "s2"}

	cs1, err := mgr.Session(ctx, key1, StartOpts{RepoDir: baseDir, Seat: key1.Seat})
	if err != nil {
		t.Fatalf("Session key1 first: %v", err)
	}
	if cs1 == nil {
		t.Fatal("expected non-nil session")
	}

	cs1Again, err := mgr.Session(ctx, key1, StartOpts{RepoDir: baseDir, Seat: key1.Seat})
	if err != nil {
		t.Fatalf("Session key1 second: %v", err)
	}
	if cs1Again != cs1 {
		t.Fatal("expected same session pointer for same key")
	}

	cs2, err := mgr.Session(ctx, key2, StartOpts{RepoDir: baseDir, Seat: key2.Seat})
	if err != nil {
		t.Fatalf("Session key2: %v", err)
	}
	if cs2 == cs1 {
		t.Fatal("expected different session pointer for different key")
	}
}

func TestManager_PromptAndSubscribe(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	mgr := NewManager(baseDir, newTestFactory(t, nil))

	key := SessionKey{Project: "p1", Seat: "s1"}
	cs, err := mgr.Session(ctx, key, StartOpts{RepoDir: baseDir, Seat: key.Seat})
	if err != nil {
		t.Fatalf("Session: %v", err)
	}

	subID, ch := cs.Subscribe()
	if subID != 0 {
		t.Fatalf("expected first subscriber id 0, got %d", subID)
	}

	if err := cs.Prompt(ctx, "hello"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	fake := unwrapFakeSession(t, cs)
	fake.Emit(Event{Kind: EventMessage, Text: "hi"})

	e := <-ch
	if e.Kind != EventMessage || e.Text != "hi" {
		t.Fatalf("unexpected event: %+v", e)
	}
}

func TestManager_Get(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	mgr := NewManager(baseDir, newTestFactory(t, nil))

	key := SessionKey{Project: "p1", Seat: "s1"}
	if _, ok := mgr.Get(key); ok {
		t.Fatal("Get on empty manager should miss")
	}

	cs, err := mgr.Session(ctx, key, StartOpts{RepoDir: baseDir, Seat: key.Seat})
	if err != nil {
		t.Fatalf("Session: %v", err)
	}

	got, ok := mgr.Get(key)
	if !ok {
		t.Fatal("Get after Session should hit")
	}
	if got != cs {
		t.Fatal("Get returned different session pointer")
	}
}

func TestManager_ListAndClose(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	mgr := NewManager(baseDir, newTestFactory(t, nil))

	key1 := SessionKey{Project: "p1", Seat: "s1"}
	key2 := SessionKey{Project: "p1", Seat: "s2"}

	if _, err := mgr.Session(ctx, key1, StartOpts{RepoDir: baseDir, Seat: key1.Seat}); err != nil {
		t.Fatalf("Session key1: %v", err)
	}
	if _, err := mgr.Session(ctx, key2, StartOpts{RepoDir: baseDir, Seat: key2.Seat}); err != nil {
		t.Fatalf("Session key2: %v", err)
	}

	list := mgr.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(list))
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Seat < list[j].Seat })
	if list[0] != key1 || list[1] != key2 {
		t.Fatalf("unexpected list: %v", list)
	}

	if err := mgr.Close(key1); err != nil {
		t.Fatalf("Close key1: %v", err)
	}
	if _, ok := mgr.Get(key1); ok {
		t.Fatal("key1 should be gone after Close")
	}
	if list := mgr.List(); len(list) != 1 {
		t.Fatalf("expected 1 key after Close, got %d", len(list))
	}

	if err := mgr.CloseAll(); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}
	if list := mgr.List(); len(list) != 0 {
		t.Fatalf("expected empty list after CloseAll, got %d", len(list))
	}
}

func TestManager_FactoryErrorDoesNotStore(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	boom := errors.New("factory failed")
	mgr := NewManager(baseDir, func(key SessionKey) (Backend, error) {
		return nil, boom
	})

	key := SessionKey{Project: "p1", Seat: "s1"}
	_, err := mgr.Session(ctx, key, StartOpts{RepoDir: baseDir, Seat: key.Seat})
	if !errors.Is(err, boom) {
		t.Fatalf("expected factory error, got %v", err)
	}
	if _, ok := mgr.Get(key); ok {
		t.Fatal("session should not be stored after factory error")
	}
}

func TestManager_ConcurrentSessionSameKey(t *testing.T) {
	baseDir := t.TempDir()
	var count atomic.Int32
	mgr := NewManager(baseDir, newTestFactory(t, &count))

	key := SessionKey{Project: "p1", Seat: "s1"}
	const n = 50

	var wg sync.WaitGroup
	results := make([]*CockpitSession, n)
	var errs [n]error

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = mgr.Session(context.Background(), key, StartOpts{RepoDir: baseDir, Seat: key.Seat})
		}(i)
	}
	wg.Wait()

	if count.Load() != 1 {
		t.Fatalf("factory called %d times, expected 1", count.Load())
	}

	var first *CockpitSession
	for i, cs := range results {
		if errs[i] != nil {
			t.Fatalf("goroutine %d error: %v", i, errs[i])
		}
		if cs == nil {
			t.Fatalf("goroutine %d got nil session", i)
		}
		if first == nil {
			first = cs
		} else if cs != first {
			t.Fatal("concurrent callers got different session pointers")
		}
	}
}

func TestManager_JsonlPath(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	mgr := NewManager(baseDir, newTestFactory(t, nil))

	key := SessionKey{Project: "p1", Seat: "s1"}
	cs, err := mgr.Session(ctx, key, StartOpts{RepoDir: baseDir, Seat: key.Seat})
	if err != nil {
		t.Fatalf("Session: %v", err)
	}

	expectedPath := filepath.Join(baseDir, "cockpit", "p1__s1.jsonl")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected jsonl file at %s: %v", expectedPath, err)
	}

	if err := cs.Prompt(ctx, "hello"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	fake := unwrapFakeSession(t, cs)
	fake.Emit(Event{Kind: EventMessage, Text: "world"})

	// Pump drains asynchronously; wait until history has the event.
	var events []Event
	for i := 0; i < 100; i++ {
		events, err = cs.History()
		if err != nil {
			t.Fatalf("History: %v", err)
		}
		if len(events) == 1 {
			break
		}
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event in jsonl, got %d", len(events))
	}
	if events[0].Text != "world" {
		t.Fatalf("unexpected event text: %q", events[0].Text)
	}
}

func TestManager_AuditSink(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()

	var mu sync.Mutex
	var gotKey SessionKey
	var gotEvent AuditEvent
	var count atomic.Int32
	sink := func(key SessionKey, ev AuditEvent) {
		mu.Lock()
		defer mu.Unlock()
		gotKey = key
		gotEvent = ev
		count.Add(1)
	}

	mgr := NewManagerCtxAudit(ctx, baseDir, newTestFactory(t, nil), sink)
	key := SessionKey{Project: "p1", Seat: "s1"}
	cs, err := mgr.Session(ctx, key, StartOpts{RepoDir: baseDir, Seat: key.Seat})
	if err != nil {
		t.Fatalf("Session: %v", err)
	}

	fake := unwrapFakeSession(t, cs)
	fake.Emit(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: "Bash", Text: "rm -rf /tmp"}})

	deadline := time.Now().Add(2 * time.Second)
	for count.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for audit sink")
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	gk, ge := gotKey, gotEvent
	mu.Unlock()

	if gk != key {
		t.Fatalf("audit key mismatch: got %+v, want %+v", gk, key)
	}
	if ge.Tool != "Bash" {
		t.Fatalf("tool mismatch: got %q, want Bash", ge.Tool)
	}
	if ge.Risk != "exec" {
		t.Fatalf("risk mismatch: got %q, want exec", ge.Risk)
	}
}

func unwrapFakeSession(t *testing.T, cs *CockpitSession) *FakeSession {
	t.Helper()
	fs, ok := cs.sess.(*FakeSession)
	if !ok {
		t.Fatalf("expected *FakeSession, got %T", cs.sess)
	}
	return fs
}

// ctxCapturingBackend records the context Start receives, to prove the manager
// spawns the backend under a session-lifetime context (not the caller's).
type ctxCapturingBackend struct{ gotCtx context.Context }

func (b *ctxCapturingBackend) Start(ctx context.Context, _ StartOpts) (Session, error) {
	b.gotCtx = ctx
	return NewFakeSession("t"), nil
}
func (b *ctxCapturingBackend) Resume(ctx context.Context, _ string) (Session, error) {
	b.gotCtx = ctx
	return NewFakeSession("t"), nil
}

// Regression: a cockpit session outlives the request that created it. If the
// backend process were spawned under the caller's (HTTP request) context, it
// would die the instant POST /prompt returned — the real "empty event stream"
// bug found while driving a live claude cockpit. Session must decouple the
// backend from the caller ctx and only tear it down on Close.
func TestManager_SessionOutlivesCallerCtx(t *testing.T) {
	be := &ctxCapturingBackend{}
	m := NewManager(t.TempDir(), func(SessionKey) (Backend, error) { return be, nil })

	callerCtx, cancel := context.WithCancel(context.Background())
	cancel() // caller ctx already dead, as if the request already returned

	key := SessionKey{Project: "p", Seat: "s"}
	if _, err := m.Session(callerCtx, key, StartOpts{}); err != nil {
		t.Fatalf("Session: %v", err)
	}
	if be.gotCtx == nil {
		t.Fatal("backend Start received nil ctx")
	}
	if be.gotCtx.Err() != nil {
		t.Fatalf("backend ctx already cancelled — process is tied to the caller ctx and would die on request end: %v", be.gotCtx.Err())
	}
	// Close tears the backend down.
	_ = m.Close(key)
	if be.gotCtx.Err() == nil {
		t.Fatal("backend ctx not cancelled after Close — session process would leak")
	}
}
