package sub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

const kimiCurrentUsage = "{\"type\":\"usage.record\",\"usageScope\":\"turn\",\"usage\":{\"inputOther\":23,\"inputCacheRead\":100,\"output\":7}}\n"

func appendKimiUsageFixture(t *testing.T, path, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Error(err)
		return
	}
	_, err = f.WriteString(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Error(err)
	}
}

func TestKimiUsageWaitsForWireBeforeClose(t *testing.T) {
	for _, marker := range []string{"prompt.completed", "turn.ended"} {
		t.Run(marker, func(t *testing.T) {
			store, s, _, h, _, home := kimiJobFixture(t, false)
			done := make(chan struct{})
			s.script = func(s *fakeSession) {
				defer close(done)
				writeKimiJobRecord(t, home, s.ThreadID(), "")
				path := filepath.Join(home, "sessions", s.ThreadID(), "agents", "main", "wire.jsonl")
				// Replace the fixture with the state observed on real kimi: the prompt has
				// returned over ACP while its wire usage is still queued for async writing.
				if err := os.WriteFile(path, []byte("{\"type\":\"turn.prompt\"}\n{\"type\":\"llm.request\",\"model\":\"current-model\"}\n"), 0600); err != nil {
					t.Error(err)
				}
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				time.Sleep(60 * time.Millisecond)
				if s.closed.Load() {
					return
				} // Close kills the writer and loses queued data.
				appendKimiUsageFixture(t, path, kimiCurrentUsage)
				time.Sleep(40 * time.Millisecond)
				if s.closed.Load() {
					t.Error("session closed before the completion marker was flushed")
					return
				}
				appendKimiUsageFixture(t, path, fmt.Sprintf("{\"type\":%q}\n", marker))
			}
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
			<-done
			if err != nil || !r.ModelVerified || r.ModelActual != "current-model" || r.InputTokens != 123 || r.OutputTokens != 7 || contains(r.Warnings, "incomplete") {
				t.Fatalf("result=%+v err=%v", r, err)
			}
			if !s.closed.Load() {
				t.Fatal("session was not closed after verification")
			}
		})
	}
}

func TestKimiUsageResumeCountsOnlyCurrentJob(t *testing.T) {
	store, s, _, h, _, home := kimiJobFixture(t, false)
	st, _ := store.Load("j1")
	st.Request.Resume = s.ThreadID()
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	writeKimiJobRecord(t, home, s.ThreadID(), "{\"type\":\"prompt.completed\"}\n{\"type\":\"turn.ended\"}\n")
	path := filepath.Join(home, "sessions", s.ThreadID(), "agents", "main", "wire.jsonl")
	s.script = func(s *fakeSession) {
		appendKimiUsageFixture(t, path, "{\"type\":\"turn.prompt\"}\n{\"type\":\"llm.request\",\"model\":\"resumed-model\"}\n"+kimiCurrentUsage+"{\"type\":\"prompt.completed\"}\n")
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil || !r.ModelVerified || r.ModelActual != "resumed-model" || r.InputTokens != 123 || r.OutputTokens != 7 {
		t.Fatalf("resumed usage includes history: %+v err=%v", r, err)
	}
}

func TestKimiUsageIncompleteWireWarns(t *testing.T) {
	store, s, _, h, _, home := kimiJobFixture(t, false)
	s.script = func(s *fakeSession) {
		writeKimiJobRecord(t, home, s.ThreadID(), "")
		path := filepath.Join(home, "sessions", s.ThreadID(), "agents", "main", "wire.jsonl")
		if err := os.WriteFile(path, []byte("{\"type\":\"turn.prompt\"}\n{\"type\":\"llm.request\",\"model\":\"partial-model\"}\n"), 0600); err != nil {
			t.Error(err)
		}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	started := time.Now()
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil || !r.ModelVerified || r.ModelActual != "partial-model" || r.InputTokens != 0 || r.OutputTokens != 0 || !contains(r.Warnings, "tokens may be incomplete") {
		t.Fatalf("missing timeout warning: %+v err=%v", r, err)
	}
	if time.Since(started) > 12*time.Second {
		t.Fatal("wire verification exceeded its bounded wait")
	}
	data, err := os.ReadFile(store.ResultPath("j1"))
	if err != nil || !strings.Contains(string(data), "tokens may be incomplete") {
		t.Fatalf("timeout warning not persisted: %s %v", data, err)
	}
}

func TestKimiUsageBadBaselineWarnsWithoutFailingWorktree(t *testing.T) {
	store, s, _, h, _, home := kimiJobFixture(t, false)
	original, _ := store.Load("j1")
	req := original.Request
	req.Resume = s.ThreadID()
	st, err := store.CreateRun("isolated", req, true, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = runGit(req.Cwd, "worktree", "remove", "--force", st.Worktree.Path) })
	writeKimiJobRecord(t, home, s.ThreadID(), "")
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte("broken index\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "isolated", h, WorkerConfig{KimiWireTimeout: 100 * time.Millisecond})
	if err != nil || r.Status != StatusSucceeded || !contains(r.Warnings, "baseline unavailable") || !contains(r.Warnings, "tokens may be incomplete") {
		t.Fatalf("accounting failure changed job outcome: %+v err=%v", r, err)
	}
}
