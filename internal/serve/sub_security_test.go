package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func resumableAPIJob(t *testing.T, store sub.Store, id, cwd string) sub.State {
	t.Helper()
	st, err := store.Create(id, sub.Request{Harness: "fake", Access: "write", Cwd: cwd})
	if err != nil {
		t.Fatal(err)
	}
	result := sub.Result{JobID: id, Status: sub.StatusSucceeded, SessionID: "session-" + id}
	if err := store.Finish(id, &result, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	st, _ = store.Load(id)
	return st
}
func TestConcurrentPlainResumeAPI(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	store, _ := sub.DefaultStore()
	parent := resumableAPIJob(t, store, "parent", actionRepo(t))
	mux := http.NewServeMux()
	server := New(nil)
	var starts atomic.Int32
	server.registerJobWrites(mux, store, func(string) error { starts.Add(1); return nil })
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 5)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			responses <- actionRequest(mux, parent.ID, "resume", `{"brief":"continue"}`, "application/json", "")
		}()
	}
	close(start)
	wg.Wait()
	close(responses)
	successful := 0
	childID := ""
	for w := range responses {
		if w.Code == 200 {
			successful++
			var response struct {
				JobID string `json:"jobId"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &response)
			childID = response.JobID
		} else if w.Code != 409 {
			t.Errorf("want 409: %d %s", w.Code, w.Body.String())
		}
	}
	if successful != 1 || starts.Load() != 1 {
		t.Fatalf("successful=%d starts=%d", successful, starts.Load())
	}
	for _, action := range store.Actions(parent) {
		if action == "resume" {
			t.Fatal("parent still offers resume while child is queued")
		}
	}
	child, _ := store.Load(childID)
	result := sub.Result{JobID: childID, Status: sub.StatusSucceeded, SessionID: child.Request.Resume}
	if err := store.Finish(childID, &result, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if w := actionRequest(mux, parent.ID, "resume", `{"brief":"old parent"}`, "application/json", ""); w.Code != 409 {
		t.Fatalf("old parent: %d %s", w.Code, w.Body.String())
	}
	child, _ = store.Load(childID)
	found := false
	for _, a := range store.Actions(child) {
		found = found || a == "resume"
	}
	if !found {
		t.Fatal("latest terminal child does not offer resume")
	}
}

func TestDashboardConcurrentJobLimit(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	store, _ := sub.DefaultStore()
	cwd := actionRepo(t)
	sources := []sub.State{}
	for i := 0; i < 8; i++ {
		sources = append(sources, resumableAPIJob(t, store, fmt.Sprintf("parent-%d", i), cwd))
	}
	// Ordinary CLI jobs do not consume dashboard capacity.
	for i := 0; i < 4; i++ {
		if _, err := store.Create(fmt.Sprintf("cli-%d", i), sub.Request{Harness: "fake", Cwd: cwd}); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	server := New(nil)
	var started atomic.Int32
	server.registerJobWrites(mux, store, func(string) error { started.Add(1); return nil })
	responses := make(chan *httptest.ResponseRecorder, 8)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, st := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			responses <- actionRequest(mux, st.ID, "resume", `{"brief":"continue"}`, "application/json", "")
		}()
	}
	close(start)
	wg.Wait()
	close(responses)
	successes, limited := 0, 0
	childID := ""
	for w := range responses {
		switch w.Code {
		case 200:
			successes++
			var got struct {
				JobID string `json:"jobId"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &got)
			childID = got.JobID
		case 429:
			limited++
			var got map[string]string
			_ = json.Unmarshal(w.Body.Bytes(), &got)
			if got["error"] != "too many jobs started from the dashboard are still running (limit 4)" {
				t.Fatal(got)
			}
		default:
			t.Errorf("%d %s", w.Code, w.Body.String())
		}
	}
	if successes != 4 || limited != 4 || started.Load() != 4 {
		t.Fatalf("successes=%d limited=%d starts=%d", successes, limited, started.Load())
	}
	if _, err := store.CancelJob(t.Context(), childID); err != nil {
		t.Fatal(err)
	}
	extra := resumableAPIJob(t, store, "extra", cwd)
	if w := actionRequest(mux, extra.ID, "resume", `{"brief":"continue"}`, "application/json", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestDashboardLaunchFailuresDoNotConsumeCapacity(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	store, _ := sub.DefaultStore()
	cwd := actionRepo(t)
	server := New(nil)
	mux := http.NewServeMux()
	server.registerJobWrites(mux, store, func(string) error { return fmt.Errorf("cannot start worker") })
	for i := 0; i < 6; i++ {
		parent := resumableAPIJob(t, store, fmt.Sprintf("failure-%d", i), cwd)
		w := actionRequest(mux, parent.ID, "resume", `{"brief":"continue"}`, "application/json", "")
		if w.Code != 500 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
