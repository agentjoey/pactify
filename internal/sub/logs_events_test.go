package sub

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

type liveChannelWriter chan string

func (w liveChannelWriter) Write(b []byte) (int, error) { w <- string(b); return len(b), nil }
func TestStructuredFollowReadsCommittedEventsAndCancels(t *testing.T) {
	store := Store{Root: t.TempDir()}
	st, err := store.Create("events", Request{Harness: "kimi"})
	if err != nil {
		t.Fatal(err)
	}
	st.Status = StatusRunning
	if err = store.Save(st); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(store.LogPath(st.ID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = io.WriteString(file, `{"kind":"message","text":"initial","final":true}`+"\n"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(liveChannelWriter, 8)
	done := make(chan error, 1)
	go func() {
		done <- store.Logs(ctx, st.ID, out, LogOptions{Tail: 100, Follow: true, ReadOnly: true, Structured: true})
	}()
	next := func(want string) {
		t.Helper()
		select {
		case got := <-out:
			if !strings.Contains(got, want) {
				t.Fatal(got)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no event")
		}
	}
	next(`"text":"initial"`)
	if _, err = io.WriteString(file, `{"kind":"message","text":"committed"`); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-out:
		t.Fatal("partial line was emitted", line)
	case <-time.After(150 * time.Millisecond):
	}
	if _, err = io.WriteString(file, "}\n"); err != nil {
		t.Fatal(err)
	}
	next(`"text":"committed"`)
	cancel()
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("did not cancel")
	}
	current, err := store.Load(st.ID)
	if err != nil || current.Status != StatusRunning {
		t.Fatal("read-only stream changed state", current, err)
	}
}

func TestStructuredUsageUsesCollectorAccounting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		want  string
		price bool
	}{
		{"codex", []string{`{"kind":"usage","raw":{"threadId":"main","tokenUsage":{"total":{"inputTokens":100,"outputTokens":2}}}}`, `{"kind":"usage","raw":{"threadId":"other","tokenUsage":{"total":{"inputTokens":50,"outputTokens":3}}}}`}, `"inputTokens":150,"outputTokens":5`, false},
		{"kimi retry", []string{`{"kind":"usage","usage":{"inputTokens":10,"outputTokens":2}}`, `{"kind":"system","state":"transient_retry","text":"kimi"}`, `{"kind":"usage","usage":{"inputTokens":4,"outputTokens":1}}`}, `"inputTokens":14,"outputTokens":3`, false},
		{"kimi empty reply followup", []string{`{"kind":"usage","usage":{"inputTokens":10,"outputTokens":2}}`, `{"kind":"system","state":"empty_reply_followup","text":"kimi"}`, `{"kind":"usage","usage":{"inputTokens":4,"outputTokens":1}}`}, `"inputTokens":14,"outputTokens":3`, false},
		{"opencode job delta", []string{`{"kind":"usage","usage":{"inputTokens":5,"outputTokens":2,"costUSD":0.25},"raw":{"accounting":"job_delta"}}`}, `"costUSD":0.25`, true},
		{"historical session cost", []string{`{"kind":"usage","usage":{"inputTokens":5,"outputTokens":2,"costUSD":2.5}}`}, `"inputTokens":5,"outputTokens":2`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &logRenderer{structured: true}
			var last string
			for _, line := range tc.lines {
				if err := r.add(line, func(s string) error { last = s; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if !strings.Contains(last, tc.want) || strings.Contains(last, `"costUSD"`) != tc.price {
				t.Fatal(last)
			}
		})
	}
}

func TestStructuredDiffCountsHeaderLikeContent(t *testing.T) {
	r := &logRenderer{structured: true}
	var got string
	err := r.add(`{"kind":"diff","raw":{"diff":"diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1,2 +1,2 @@\n--- old\n-old\n+++ new\n+new\n"}}`, func(s string) error { got = s; return nil })
	if err != nil || !strings.Contains(got, `"files":1,"added":2,"deleted":2`) {
		t.Fatal(got, err)
	}
}
