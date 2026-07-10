package serve

import (
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cloudauth"
	"github.com/agentjoey/pactify/internal/cockpit"
)

type recordEmitter struct {
	mu    sync.Mutex
	emits []emitRecord
}

type emitRecord struct {
	agentKind string
	msg       any
}

func (r *recordEmitter) emit(agentKind string, msg any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.emits = append(r.emits, emitRecord{agentKind: agentKind, msg: msg})
	return nil
}

func (r *recordEmitter) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.emits)
}

func (r *recordEmitter) at(i int) emitRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.emits[i]
}

func fakeEncrypt(project string, payload []byte) (cloudauth.EncryptedBlob, error) {
	return cloudauth.EncryptedBlob{
		Alg:   "xchacha20poly1305",
		Nonce: base64.StdEncoding.EncodeToString([]byte("nonce1234567890123456789")),
		Ct:    base64.StdEncoding.EncodeToString(payload),
	}, nil
}

func decodeBody(blob cloudauth.EncryptedBlob) ([]byte, error) {
	return base64.StdEncoding.DecodeString(blob.Ct)
}

func TestCockpitRemoteSubscribeAndMirror(t *testing.T) {
	srv, fake, _ := newCockpitTestServer(t)
	rec := &recordEmitter{}
	sc := newServeCockpiter(srv, "machine-1", rec.emit, fakeEncrypt)

	if err := sc.Subscribe("p", "claude"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// The first emitted message is the status snapshot.
	eventually(t, func() bool { return rec.len() >= 1 }, 2*time.Second, 10*time.Millisecond)

	snap := rec.at(0)
	if snap.agentKind != "claude" {
		t.Fatalf("snapshot agentKind = %q, want claude", snap.agentKind)
	}
	header, body := parseWireMsg(t, snap.msg)
	if header.EventKind != "snapshot" || header.RunID != "cockpit:p:claude" {
		t.Fatalf("snapshot header wrong: %+v", header)
	}
	if !header.Ephemeral {
		t.Fatal("snapshot must be ephemeral")
	}
	var snapBody mirrorStatusBody
	if err := json.Unmarshal(decodeBodyT(t, body), &snapBody); err != nil {
		t.Fatalf("decode snapshot body: %v", err)
	}
	if snapBody.Kind != "status" {
		t.Fatalf("snapshot body kind = %q, want status", snapBody.Kind)
	}

	// Prompt the session and emit a cockpit event; it should be mirrored.
	if err := sc.Prompt("p", "claude", "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	fake.Emit(cockpit.Event{Kind: cockpit.EventMessage, Text: "hello"})

	eventually(t, func() bool { return rec.len() >= 2 }, 2*time.Second, 10*time.Millisecond)

	msg := rec.at(1)
	if msg.agentKind != "claude" {
		t.Fatalf("event agentKind = %q, want claude", msg.agentKind)
	}
	header, body = parseWireMsg(t, msg.msg)
	if header.EventKind != "message" || header.State != "thinking" {
		t.Fatalf("message header wrong: %+v", header)
	}
	var ev cockpit.Event
	if err := json.Unmarshal(decodeBodyT(t, body), &ev); err != nil {
		t.Fatalf("decode event body: %v", err)
	}
	if ev.Kind != cockpit.EventMessage || ev.Text != "hello" {
		t.Fatalf("event body wrong: %+v", ev)
	}
}

func TestCockpitRemotePermissionEmitsResolved(t *testing.T) {
	srv, fake, _ := newCockpitTestServer(t)
	rec := &recordEmitter{}
	sc := newServeCockpiter(srv, "machine-1", rec.emit, fakeEncrypt)

	// Subscribe first so the permission resolved event has somewhere to go.
	if err := sc.Subscribe("p", "claude"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	eventually(t, func() bool { return rec.len() >= 1 }, 2*time.Second, 10*time.Millisecond)

	// Create a session and an approval request.
	if err := sc.Prompt("p", "claude", "do it"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	var gotDecision cockpit.Decision
	fake.EmitApproval(cockpit.ApprovalRequest{
		Kind:     "command",
		ToolName: "ls",
		RawInput: json.RawMessage(`{"path":"/etc"}`),
		Respond: func(d cockpit.Decision) error {
			gotDecision = d
			return nil
		},
	})
	eventually(t, func() bool {
		cs, ok := srv.cockpit.Get(cockpit.SessionKey{Project: "p", Seat: "claude"})
		if !ok {
			return false
		}
		return len(cs.Pending()) == 1
	}, 2*time.Second, 10*time.Millisecond)

	cs, _ := srv.cockpit.Get(cockpit.SessionKey{Project: "p", Seat: "claude"})
	pending := cs.Pending()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending approval, got %d", len(pending))
	}

	// Resolve it remotely.
	if err := sc.Permission("p", "claude", pending[0].ID, "allow"); err != nil {
		t.Fatalf("permission: %v", err)
	}
	if gotDecision != cockpit.DecisionAllow {
		t.Fatalf("decision = %q, want allow", gotDecision)
	}

	// Wait for the approval-resolved mirror event.
	eventually(t, func() bool {
		for i := 0; i < rec.len(); i++ {
			h, _ := parseWireMsg(t, rec.at(i).msg)
			if h.EventKind == "approval-resolved" {
				return true
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond)
}

func TestCockpitRemoteSubscribeTTLStops(t *testing.T) {
	srv, fake, _ := newCockpitTestServer(t)
	rec := &recordEmitter{}
	sc := newServeCockpiter(srv, "machine-1", rec.emit, fakeEncrypt)

	if err := sc.Subscribe("p", "claude"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	eventually(t, func() bool { return rec.len() >= 1 }, 2*time.Second, 10*time.Millisecond)

	// Shorten the TTL on the active mirror so the test doesn't wait 5 minutes.
	sc.mu.Lock()
	sub := sc.mirrors[cockpit.SessionKey{Project: "p", Seat: "claude"}]
	if sub == nil {
		sc.mu.Unlock()
		t.Fatal("mirror not registered")
	}
	sub.ttl = 50 * time.Millisecond
	sc.mu.Unlock()
	sub.renew()

	// After TTL fires, further cockpit events must not be mirrored.
	time.Sleep(200 * time.Millisecond)
	before := rec.len()
	fake.Emit(cockpit.Event{Kind: cockpit.EventMessage, Text: "after ttl"})
	time.Sleep(100 * time.Millisecond)
	if rec.len() != before {
		t.Fatalf("mirror emitted after TTL expired: before=%d after=%d", before, rec.len())
	}
}

func TestCockpitRemoteUnknownProjectNoEmit(t *testing.T) {
	srv, _, _ := newCockpitTestServer(t)
	rec := &recordEmitter{}
	sc := newServeCockpiter(srv, "machine-1", rec.emit, fakeEncrypt)

	if err := sc.Subscribe("unknown", "claude"); err == nil {
		t.Fatal("subscribe for unknown project should fail")
	}
	if rec.len() != 0 {
		t.Fatalf("no emit expected for unknown project, got %d", rec.len())
	}
}

func parseWireMsg(t *testing.T, msg any) (wireHeader, cloudauth.EncryptedBlob) {
	t.Helper()
	m, ok := msg.(map[string]any)
	if !ok {
		t.Fatalf("msg is not a map: %T", msg)
	}
	hJSON, err := json.Marshal(m["header"])
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	bJSON, err := json.Marshal(m["body"])
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	var h wireHeader
	if err := json.Unmarshal(hJSON, &h); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	var b cloudauth.EncryptedBlob
	if err := json.Unmarshal(bJSON, &b); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return h, b
}

func decodeBodyT(t *testing.T, blob cloudauth.EncryptedBlob) []byte {
	t.Helper()
	b, err := decodeBody(blob)
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return b
}

func eventually(t *testing.T, cond func() bool, timeout, interval time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(interval)
	}
	t.Fatal("condition never satisfied")
}

// TestMapCockpitEventStaysInsideWireEnums pins every mapCockpitEvent output to
// the cloud/wire OperationalHeader enums (RunState / EventKind). The relay
// strict-parses the header and silently drops non-conforming messages, so an
// out-of-enum value here is a silent data-loss bug, not a cosmetic one.
func TestMapCockpitEventStaysInsideWireEnums(t *testing.T) {
	wireStates := map[string]bool{
		"idle": true, "thinking": true, "blocked": true,
		"awaiting-approval": true, "done": true, "error": true,
	}
	wireKinds := map[string]bool{
		"snapshot": true, "delta": true, "message": true, "thinking": true,
		"approval-request": true, "approval-resolved": true,
		"run-started": true, "run-ended": true,
	}
	cases := []cockpit.Event{
		{Kind: cockpit.EventState, State: "turn_completed"},
		{Kind: cockpit.EventState, State: "turn_failed"},
		{Kind: cockpit.EventState, State: "turn_started"},
		{Kind: cockpit.EventError, Text: "boom"},
		{Kind: cockpit.EventMessage, Text: "hi"},
		{Kind: cockpit.EventUsage},
		{Kind: cockpit.EventSystem, Text: "queue full"},
	}
	for _, e := range cases {
		state, kind := mapCockpitEvent(e)
		if !wireStates[state] {
			t.Errorf("mapCockpitEvent(%v) state %q not a wire RunState", e.Kind, state)
		}
		if !wireKinds[kind] {
			t.Errorf("mapCockpitEvent(%v) eventKind %q not a wire EventKind", e.Kind, kind)
		}
	}
}
