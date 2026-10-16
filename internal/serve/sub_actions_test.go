package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func actionRequest(h http.Handler, id, action, body, ct, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/sub/jobs/"+id+"/"+action, strings.NewReader(body))
	r.Header.Set("Content-Type", ct)
	r.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func actionRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "test"}, {"commit", "--allow-empty", "-qm", "base"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	return dir
}
func TestJobActionAPI(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	store, _ := sub.DefaultStore()
	server := New(nil)
	started := ""
	mux := http.NewServeMux()
	server.registerJobWrites(mux, store, func(id string) error { started = id; return nil })
	h := writeGuard(mux)
	call := func(id, action, body string) *httptest.ResponseRecorder {
		return actionRequest(h, id, action, body, "application/json", "")
	}
	st, err := store.CreateRun("old", sub.Request{Harness: "fake", Access: "write", Model: "chosen", Effort: "high", Cwd: actionRepo(t)}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.DiscardWorktree(st.ID) })
	for _, action := range []string{"resume", "apply", "discard"} {
		w := call(st.ID, action, `{"brief":"more"}`)
		if w.Code != 409 {
			t.Fatalf("running %s: %d %s", action, w.Code, w.Body.String())
		}
	}
	w := call(st.ID, "cancel", `{}`)
	if w.Code != 200 {
		t.Fatalf("cancel %d %s", w.Code, w.Body.String())
	}
	for _, action := range []string{"cancel", "resume"} {
		w := call(st.ID, action, `{"brief":"more"}`)
		if w.Code != 409 {
			t.Fatalf("no session %s: %d %s", action, w.Code, w.Body.String())
		}
	}
	// Publish a resumable terminal fixture without launching a billable harness.
	st, _ = store.Load(st.ID)
	result := sub.Result{JobID: st.ID, Status: st.Status, SessionID: "fixture-session"}
	data, _ := json.Marshal(result)
	if err := os.WriteFile(store.ResultPath(st.ID), data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, brief := range []string{"", " \n\t"} {
		body, _ := json.Marshal(map[string]string{"brief": brief})
		if w := call(st.ID, "resume", string(body)); w.Code != 400 {
			t.Fatalf("empty brief: %d %s", w.Code, w.Body.String())
		}
	}
	marker := filepath.Join(t.TempDir(), "executed")
	brief := "$(touch " + marker + "); `touch " + marker + "` & > " + marker
	body, _ := json.Marshal(map[string]string{"brief": brief})
	w = call(st.ID, "resume", string(body))
	var response struct {
		JobID string `json:"jobId"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || started != response.JobID || started == "" {
		t.Fatalf("resume: %d %s", w.Code, w.Body.String())
	}
	child, err := store.Load(response.JobID)
	if err != nil || child.Request.Brief != brief || child.Request.Model != "chosen" || child.Request.Effort != "high" || child.Worktree.Path != st.Worktree.Path || child.Request.Resume != "fixture-session" {
		t.Fatalf("%+v %v", child, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("brief was executed", err)
	}
	for _, action := range []string{"resume", "apply", "discard"} {
		if w := call(st.ID, action, `{"brief":"more"}`); w.Code != 409 {
			t.Fatalf("busy %s: %d %s", action, w.Code, w.Body.String())
		}
	}
	if w := call(child.ID, "cancel", `{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := os.WriteFile(filepath.Join(st.Worktree.Path, "added"), []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Conflict preserves the original workspace and leaves apply available.
	original := filepath.Join(st.Worktree.OriginalRoot, "added")
	if err := os.WriteFile(original, []byte("user\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w = call(st.ID, "apply", `{}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"error":`) {
		t.Fatalf("conflict: %d %s", w.Code, w.Body.String())
	}
	unchanged, _ := os.ReadFile(original)
	if string(unchanged) != "user\n" {
		t.Fatal("overwrote conflict")
	}
	_ = os.Remove(original)
	w = call(st.ID, "apply", `{}`)
	if w.Code != 200 {
		t.Fatalf("apply: %d %s", w.Code, w.Body.String())
	}
	applied, _ := os.ReadFile(original)
	if string(applied) != "new\n" {
		t.Fatal(string(applied))
	}
	if w := call(st.ID, "apply", `{}`); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	st, _ = store.Load(st.ID)
	if got := store.Summarize(st, nil, time.Now()).Actions; !reflect.DeepEqual(got, []string{"discard"}) {
		t.Fatal(got)
	}
	if w := call(st.ID, "discard", `{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, action := range []string{"apply", "discard", "resume"} {
		if w := call(st.ID, action, `{"brief":"more"}`); w.Code != 409 {
			t.Fatalf("discarded %s: %d %s", action, w.Code, w.Body.String())
		}
	}
	if _, err := os.Stat(st.Worktree.Path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
func TestJobActionRequestGuards(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_ALLOWED_ORIGINS", "")
	h := New(nil).Handler()
	for _, action := range []string{"cancel", "resume", "apply", "discard"} {
		for _, tc := range []struct {
			body, ct, origin string
			code             int
		}{
			{`{}`, "application/json", "https://evil.example", 403},
			{`{}`, "text/plain", "", 415},
			{strings.Repeat(" ", 65537), "application/json", "", 413},
			{`{} {}`, "application/json", "", 400},
			{`{`, "application/json", "", 400},
			{`null`, "application/json", "", 400},
		} {
			w := actionRequest(h, "missing", action, tc.body, tc.ct, tc.origin)
			if w.Code != tc.code || !strings.Contains(w.Body.String(), `"error":`) {
				t.Errorf("%s want %d: %d %s", action, tc.code, w.Code, w.Body.String())
			}
		}
	}
}
func TestResumeLaunchFailure(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	store, _ := sub.DefaultStore()
	st, err := store.Create("old", sub.Request{Harness: "fake", Cwd: actionRepo(t)})
	if err != nil {
		t.Fatal(err)
	}
	result := sub.Result{JobID: st.ID, Status: sub.StatusSucceeded, SessionID: "session"}
	if err := store.Finish(st.ID, &result, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server := New(nil)
	child := ""
	server.registerJobWrites(mux, store, func(id string) error { child = id; return errors.New("cannot launch worker") })
	w := actionRequest(mux, st.ID, "resume", `{"brief":"more"}`, "application/json", "")
	if w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	failed, err := store.Load(child)
	if err != nil || failed.Status != sub.StatusFailed {
		t.Fatal(failed, err)
	}
}
