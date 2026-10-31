package serve

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestDismissJobAPI(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "")
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	h := serveHandler(t, New(), "127.0.0.1:7777")
	st, err := store.Create("failed", sub.Request{})
	if err != nil {
		t.Fatal(err)
	}
	result := sub.Result{JobID: st.ID, Status: sub.StatusFailed, Warnings: []string{"review result"}}
	if err := store.Finish(st.ID, &result, "failed", time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.ResultPath(st.ID))
	if err != nil {
		t.Fatal(err)
	}
	original, _ := store.Load(st.ID)
	for _, tc := range []struct {
		body, ct, origin string
		code             int
	}{
		{`{}`, "application/json", "https://evil.example", 403},
		{`{}`, "text/plain", "", 415},
		{strings.Repeat(" ", 65537), "application/json", "", 413},
		{`{} {}`, "application/json", "", 400},
		{`null`, "application/json", "", 400},
		{`{"unexpected":true}`, "application/json", "", 400},
	} {
		w := actionRequest(h, st.ID, "dismiss", tc.body, tc.ct, tc.origin)
		if w.Code != tc.code {
			t.Errorf("guard want %d, got %d %s", tc.code, w.Code, w.Body.String())
		}
	}
	w := actionRequest(h, st.ID, "dismiss", `{}`, "application/json", "")
	if w.Code != 200 {
		t.Fatalf("dismiss: %d %s", w.Code, w.Body.String())
	}
	after, err := os.ReadFile(store.ResultPath(st.ID))
	if err != nil {
		t.Fatal(err)
	}
	dismissed, _ := store.Load(st.ID)
	if dismissed.DismissedAt.IsZero() || dismissed.Status != original.Status || !reflect.DeepEqual(dismissed.Worktree, original.Worktree) || !bytes.Equal(before, after) {
		t.Fatal("dismiss changed the result/worktree or failed to acknowledge", dismissed)
	}
	if attention := store.Summarize(dismissed, nil, time.Now()).Attention; len(attention) != 0 {
		t.Fatal(attention)
	}
	if w := actionRequest(h, st.ID, "dismiss", `{}`, "application/json", ""); w.Code != 409 {
		t.Fatalf("repeat: %d %s", w.Code, w.Body.String())
	}
	if w := actionRequest(h, "missing", "dismiss", `{}`, "application/json", ""); w.Code != 404 {
		t.Fatalf("missing: %d %s", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		name     string
		status   sub.Status
		worktree *sub.Worktree
	}{
		{name: "running", status: sub.StatusRunning},
		{name: "clean", status: sub.StatusSucceeded},
		{name: "applied", status: sub.StatusFailed, worktree: &sub.Worktree{Applied: true}},
		{name: "discarded", status: sub.StatusFailed, worktree: &sub.Worktree{Discarded: true}},
	} {
		st, err := store.Create(tc.name, sub.Request{})
		if err != nil {
			t.Fatal(err)
		}
		st.Status, st.Worktree = tc.status, tc.worktree
		if err := store.Save(st); err != nil {
			t.Fatal(err)
		}
		w := actionRequest(h, st.ID, "dismiss", `{}`, "application/json", "")
		if w.Code != 409 {
			t.Errorf("%s: %d %s", tc.name, w.Code, w.Body.String())
		}
		current, _ := store.Load(st.ID)
		if !current.DismissedAt.IsZero() || !reflect.DeepEqual(current.Worktree, st.Worktree) {
			t.Errorf("rejected dismiss mutated %s", tc.name)
		}
	}
}

func TestDismissJobAPIKeepsPendingWorktree(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "")
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	h := serveHandler(t, New(), "127.0.0.1:7777")
	st, err := store.Create("pending-failed", sub.Request{})
	if err != nil {
		t.Fatal(err)
	}
	st.Status, st.Worktree = sub.StatusFailed, &sub.Worktree{Path: "pending-path"}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	if w := actionRequest(h, st.ID, "dismiss", `{}`, "application/json", ""); w.Code != 200 {
		t.Fatalf("dismiss: %d %s", w.Code, w.Body.String())
	}
	current, err := store.Load(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.DismissedAt.IsZero() || !reflect.DeepEqual(current.Worktree, st.Worktree) {
		t.Fatalf("dismiss state = %+v", current)
	}
}
