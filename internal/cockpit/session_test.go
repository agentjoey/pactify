package cockpit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCockpitSessionSubscribeEmitAndHistory(t *testing.T) {
	fs := NewFakeSession("thread-1")
	jsonlPath := t.TempDir() + "/events.jsonl"
	cs, err := NewCockpitSession(fs, jsonlPath)
	if err != nil {
		t.Fatalf("NewCockpitSession: %v", err)
	}
	defer cs.Close()

	id, ch := cs.Subscribe()
	defer cs.Unsubscribe(id)

	events := []Event{
		{Kind: EventMessage, Text: "hello", Raw: json.RawMessage(`{}`)},
		{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: "ls"}, Raw: json.RawMessage(`{}`)},
		{Kind: EventUsage, Usage: &Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}, Raw: json.RawMessage(`{}`)},
	}
	for _, e := range events {
		fs.Emit(e)
	}

	var got []Event
	for i := 0; i < len(events); i++ {
		select {
		case e := <-ch:
			got = append(got, e)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}

	if !reflect.DeepEqual(got, events) {
		t.Fatalf("subscriber events mismatch:\ngot:  %+v\nwant: %+v", got, events)
	}

	history, err := cs.History()
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if !reflect.DeepEqual(history, events) {
		t.Fatalf("history mismatch:\ngot:  %+v\nwant: %+v", history, events)
	}
}

func TestCockpitSessionApprovalRespondAndErrors(t *testing.T) {
	fs := NewFakeSession("thread-2")
	cs, err := NewCockpitSession(fs, t.TempDir()+"/events.jsonl")
	if err != nil {
		t.Fatalf("NewCockpitSession: %v", err)
	}
	defer cs.Close()

	var called atomic.Bool
	var gotDecision Decision
	fs.EmitApproval(ApprovalRequest{
		Kind:     "command",
		ToolName: "rm",
		RawInput: json.RawMessage(`{"path":"/tmp/x"}`),
		Respond: func(d Decision) error {
			called.Store(true)
			gotDecision = d
			return nil
		},
	})

	// Wait for the approval to land in the pending queue.
	var id string
	for {
		pending := cs.Pending()
		if len(pending) == 1 {
			id = pending[0].ID
			break
		}
		select {
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for pending approval")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	if id != "ap1" {
		t.Fatalf("expected stable id ap1, got %q", id)
	}

	if err := cs.Respond("unknown", DecisionAllow); err == nil {
		t.Fatalf("Respond unknown id should error")
	}

	if err := cs.Respond(id, DecisionAllow); err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if !called.Load() {
		t.Fatalf("underlying Respond was not called")
	}
	if gotDecision != DecisionAllow {
		t.Fatalf("decision mismatch: got %q, want allow", gotDecision)
	}
	if len(cs.Pending()) != 0 {
		t.Fatalf("pending should be empty after respond")
	}

	if err := cs.Respond(id, DecisionDeny); err == nil {
		t.Fatalf("duplicate Respond should error")
	}
}

func TestCockpitSessionPromptAndThreadID(t *testing.T) {
	fs := NewFakeSession("thread-3")
	cs, err := NewCockpitSession(fs, t.TempDir()+"/events.jsonl")
	if err != nil {
		t.Fatalf("NewCockpitSession: %v", err)
	}
	defer cs.Close()

	if cs.ThreadID() != "thread-3" {
		t.Fatalf("ThreadID mismatch: got %q", cs.ThreadID())
	}

	ctx := context.Background()
	if err := cs.Prompt(ctx, "hi"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if err := cs.Prompt(ctx, "again"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	want := []UserMessage{{Text: "hi"}, {Text: "again"}}
	if !reflect.DeepEqual(fs.Prompts, want) {
		t.Fatalf("prompts mismatch: got %+v, want %+v", fs.Prompts, want)
	}
}

func TestCockpitSessionInterrupt(t *testing.T) {
	fs := NewFakeSession("thread-4")
	cs, err := NewCockpitSession(fs, t.TempDir()+"/events.jsonl")
	if err != nil {
		t.Fatalf("NewCockpitSession: %v", err)
	}
	defer cs.Close()

	if err := cs.Interrupt(context.Background()); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	if !fs.InterruptCalled {
		t.Fatalf("Interrupt was not forwarded")
	}
}

func TestCockpitSessionCloseIdempotent(t *testing.T) {
	fs := NewFakeSession("thread-5")
	cs, err := NewCockpitSession(fs, t.TempDir()+"/events.jsonl")
	if err != nil {
		t.Fatalf("NewCockpitSession: %v", err)
	}

	id, ch := cs.Subscribe()
	defer cs.Unsubscribe(id)

	if err := cs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := cs.Close(); err != nil {
		t.Fatalf("second Close should be idempotent: %v", err)
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("subscriber channel should be closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for subscriber channel to close")
	}
}

func TestCockpitSessionConcurrentSubscribersAndEmit(t *testing.T) {
	fs := NewFakeSession("thread-6")
	cs, err := NewCockpitSession(fs, t.TempDir()+"/events.jsonl")
	if err != nil {
		t.Fatalf("NewCockpitSession: %v", err)
	}
	defer cs.Close()

	const nSubs = 10
	const nEvents = 200

	var ids []int
	var chans []<-chan Event
	for i := 0; i < nSubs; i++ {
		id, ch := cs.Subscribe()
		ids = append(ids, id)
		chans = append(chans, ch)
	}
	defer func() {
		for _, id := range ids {
			cs.Unsubscribe(id)
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < nEvents; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fs.Emit(Event{Kind: EventMessage, Text: "msg", Final: idx%2 == 0, Raw: json.RawMessage(`{}`)})
		}(i)
	}
	wg.Wait()

	// Wait until all events are persisted before draining subscribers.
	var history []Event
	for {
		history, err = cs.History()
		if err != nil {
			t.Fatalf("History: %v", err)
		}
		if len(history) == nEvents {
			break
		}
		if len(history) > nEvents {
			t.Fatalf("too many events persisted: got %d, want %d", len(history), nEvents)
		}
		time.Sleep(5 * time.Millisecond)
	}

	for i, ch := range chans {
		drainWithTimeout(t, ch, ids[i])
	}
}

func TestCockpitSessionFilePermissions(t *testing.T) {
	fs := NewFakeSession("thread-perms")
	dir := t.TempDir()
	jsonlPath := dir + "/sub/events.jsonl"
	cs, err := NewCockpitSession(fs, jsonlPath)
	if err != nil {
		t.Fatalf("NewCockpitSession: %v", err)
	}
	defer cs.Close()

	fs.Emit(Event{Kind: EventMessage, Text: "x", Raw: json.RawMessage(`{}`)})
	// Wait for the event pump to write.
	for {
		history, err := cs.History()
		if err != nil {
			t.Fatalf("History: %v", err)
		}
		if len(history) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	info, err := os.Stat(jsonlPath)
	if err != nil {
		t.Fatalf("stat jsonl: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("jsonl perms want 0o600, got 0o%o", info.Mode().Perm())
	}

	dirInfo, err := os.Stat(filepath.Dir(jsonlPath))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir perms want 0o700, got 0o%o", dirInfo.Mode().Perm())
	}
}

func drainWithTimeout(t *testing.T, ch <-chan Event, id int) {
	t.Helper()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-time.After(500 * time.Millisecond):
			return
		}
	}
}
