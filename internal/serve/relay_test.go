package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRelayNilOnEmptyURL(t *testing.T) {
	r := newRelay("", "")
	if r != nil {
		t.Fatal("newRelay(\"\",\"\") must return nil")
	}
}

func TestRelayNilEnqueueNoPanic(t *testing.T) {
	var r *relay
	r.enqueue("p", `{}`) // must not panic
}

func TestRelayAuthHeader(t *testing.T) {
	// The handler runs on the server goroutine — hand results back over a
	// channel (never a bare shared variable + Sleep, which races under -race).
	authCh := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCh <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := newRelay(srv.URL, "secret-token")
	if r == nil {
		t.Fatal("newRelay with URL must return non-nil")
	}
	defer r.stop()

	r.enqueue("p", `{"event":"x"}`)
	select {
	case gotAuth := <-authCh:
		if gotAuth != "Bearer secret-token" {
			t.Fatalf("Authorization = %q, want Bearer secret-token", gotAuth)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("relay never posted to the server")
	}
}

func TestRelayBodyEnvelope(t *testing.T) {
	bodyCh := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		bodyCh <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := newRelay(srv.URL, "")
	defer r.stop()

	r.enqueue("pactify", `{"event_id":"1","type":"test"}`)
	var gotBody []byte
	select {
	case gotBody = <-bodyCh:
	case <-time.After(5 * time.Second):
		t.Fatal("relay never posted to the server")
	}

	var envelope struct {
		Project string          `json:"project"`
		Event   json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal(gotBody, &envelope); err != nil {
		t.Fatalf("invalid body JSON: %v, body=%s", err, gotBody)
	}
	if envelope.Project != "pactify" {
		t.Fatalf("project = %q, want pactify", envelope.Project)
	}
	if string(envelope.Event) != `{"event_id":"1","type":"test"}` {
		t.Fatalf("event = %s", envelope.Event)
	}
}

func TestRelayQueueFullDropsOldest(t *testing.T) {
	const cap = 256
	const N = 100

	ready := make(chan struct{})
	block := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(ready) })
		<-block
		w.WriteHeader(http.StatusOK)
	}))

	r := newRelay(srv.URL, "")
	r.enqueue("p", `"first"`) // bait: one item for the worker to pick up and block on
	<-ready                   // wait until worker has dequeued it and is blocked in handler

	for i := 0; i < cap+N; i++ {
		r.enqueue("p", `{"i":`+strconv.Itoa(i)+`}`)
	}

	dropped := atomic.LoadInt64(&r.dropped)
	if dropped < int64(N) {
		t.Fatalf("dropped = %d, want >= %d (1 dequeued + %d pushed, cap %d)", dropped, N, cap+N, cap)
	}
	if n := r.queueLen(); n > cap {
		t.Fatalf("queue len = %d, want <= %d", n, cap)
	}

	close(block)
	srv.Close()
	r.stop()
}

func TestRelayServerErrorRetriesAndDrops(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := newRelay(srv.URL, "")
	defer r.stop()

	r.enqueue("p", `{"x":1}`)

	time.Sleep(10 * time.Second)

	att := atomic.LoadInt32(&attempts)
	if att < 1 {
		t.Fatal("server was never called")
	}
	// 1 initial + 3 retries = 4 total
	if att != 4 {
		t.Fatalf("attempts = %d, want 4 (1 initial + 3 retries)", att)
	}

	dropped := atomic.LoadInt64(&r.dropped)
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1 (final failure after retries)", dropped)
	}
}

func TestRelayBadJSONLineSafe(t *testing.T) {
	bodyCh := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		bodyCh <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := newRelay(srv.URL, "")
	defer r.stop()

	r.enqueue("p", `not json`)
	var gotBody []byte
	select {
	case gotBody = <-bodyCh:
	case <-time.After(5 * time.Second):
		t.Fatal("relay never posted to the server")
	}

	if len(gotBody) == 0 {
		t.Fatal("expected a body even for bad JSON line")
	}
	var envelope struct {
		Project string          `json:"project"`
		Event   json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal(gotBody, &envelope); err != nil {
		t.Fatalf("envelope must be valid JSON: %v, body=%s", err, gotBody)
	}
	if envelope.Project != "p" {
		t.Fatalf("project = %q, want p", envelope.Project)
	}
}
