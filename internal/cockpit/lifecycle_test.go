package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The helper is a real stdio subprocess; it never invokes an installed vendor CLI.
func TestLifecycleCodexPeer(t *testing.T) {
	if os.Getenv("LIFECYCLE_CODEX_PEER") != "1" {
		return
	}
	in := json.NewDecoder(os.Stdin)
	for {
		var m wireMessage
		if in.Decode(&m) != nil {
			os.Exit(0)
		}
		if m.ID == 0 {
			continue
		}
		result := any(map[string]any{})
		switch m.Method {
		case "thread/start", "thread/resume":
			result = map[string]any{"thread": map[string]any{"id": "created-thread"}, "model": "actual"}
		case "thread/archive":
			if err := os.WriteFile(os.Getenv("LIFECYCLE_ARCHIVE"), []byte("created-thread"), 0600); err != nil {
				os.Exit(4)
			}
		}
		mustWriteJSON(os.Stdout, map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
	}
}

func TestLifecycleCodexStartFailureArchivesHiddenOnly(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("LIFECYCLE_CODEX_PEER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "' -test.run='^TestLifecycleCodexPeer$'\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, hidden := range []bool{true, false} {
		for _, resume := range []bool{false, true} {
			t.Run(fmt.Sprintf("hidden=%v/resume=%v", hidden, resume), func(t *testing.T) {
				archive := filepath.Join(t.TempDir(), "archive")
				t.Setenv("LIFECYCLE_ARCHIVE", archive)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				opts := StartOpts{RepoDir: t.TempDir(), Hidden: hidden, Model: "unavailable"}
				b := NewCodexBackend()
				var sess Session
				var err error
				if resume {
					sess, err = b.(OptsResumer).ResumeWith(ctx, "created-thread", opts)
				} else {
					sess, err = b.Start(ctx, opts)
				}
				if err == nil {
					t.Fatalf("sess=%v err=%v", sess, err)
				}
				data, e := os.ReadFile(archive)
				if hidden && (e != nil || string(data) != "created-thread") {
					t.Fatalf("created hidden thread leaked: %q %v", data, e)
				}
				if !hidden && !os.IsNotExist(e) {
					t.Fatalf("serve thread was archived: %q %v", data, e)
				}
			})
		}
	}
}

func TestLifecycleACPStartErrorIdentifiesCreatedSession(t *testing.T) {
	setupACPProcess(t)
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprintf("resume=%v", resume), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			opts := StartOpts{RepoDir: t.TempDir(), Hidden: true, Model: "unavailable"}
			b := NewACPBackend("kimi-cli")
			var err error
			if resume {
				t.Setenv("COCKPIT_ACP_WRONG_MODEL", "1")
				opts.Model = "kimi-code/kimi-for-coding"
				_, err = b.(OptsResumer).ResumeWith(ctx, "s1", opts)
			} else {
				_, err = b.Start(ctx, opts)
			}
			identified, ok := err.(interface{ SessionID() string })
			if !ok || identified.SessionID() != "s1" {
				t.Fatalf("lost created session on startup error: %v", err)
			}
		})
	}
}

func TestLifecycleCodexInterruptWaitsForInFlightStart(t *testing.T) {
	server := newFakeCodexServer(t)
	started, release := make(chan struct{}), make(chan struct{})
	server.handle("turn/start", func(wireMessage) any {
		close(started)
		<-release
		return map[string]any{"turn": map[string]any{"id": "late-turn"}}
	})
	interrupted := make(chan string, 1)
	server.handle("turn/interrupt", func(m wireMessage) any {
		var p struct {
			TurnID string `json:"turnId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		interrupted <- p.TurnID
		return map[string]any{}
	})
	sess := newCodexSessionWithClient(server.client, "thread")
	sess.hidden = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	promptDone := make(chan error, 1)
	go func() { promptDone <- sess.Prompt(ctx, UserMessage{Text: "work"}) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("prompt not sent")
	}
	cancel()
	interruptDone := make(chan error, 1)
	go func() { interruptDone <- sess.Interrupt(context.Background()) }()
	// Hold the start response until the cancellation and interrupt have both raced it.
	select {
	case <-interruptDone:
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case id := <-interrupted:
		if id != "late-turn" {
			t.Fatalf("interrupted turn %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight start escaped interruption after late turn ID")
	}
	select {
	case <-promptDone:
	case <-time.After(time.Second):
		t.Fatal("prompt did not finish")
	}
}

func TestLifecycleCanceledCodexPromptNeverReachesWire(t *testing.T) {
	server := newFakeCodexServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = server.client.turnStart(ctx, "thread", "must not start")
	if _, err := server.client.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	_, sent := server.seen["turn/start"]
	server.mu.Unlock()
	if sent {
		t.Fatal("canceled prompt reached codex wire")
	}
}

func TestLifecycleCanceledACPPromptNeverReachesWire(t *testing.T) {
	setupACPProcess(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := NewACPBackend("kimi-cli").Start(ctx, StartOpts{RepoDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	canceled, stop := context.WithCancel(ctx)
	stop()
	_ = sess.Prompt(canceled, UserMessage{Text: "must not start"})
	if err := sess.Prompt(ctx, UserMessage{Text: "barrier"}); err != nil {
		t.Fatal(err)
	}

	var final string
	for {
		select {
		case e := <-sess.Events():
			if e.Kind == EventMessage {
				final = e.Text
			}
			if e.Kind == EventState && e.State == StateTurnCompleted {
				if strings.Count(final, "session/prompt") != 1 {
					t.Fatalf("canceled ACP prompt reached wire: %s", final)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
