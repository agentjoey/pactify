package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := newRelay(srv.URL, "secret-token")
	if r == nil {
		t.Fatal("newRelay with URL must return non-nil")
	}
	r.start()
	defer r.stop()

	r.enqueue("p", `{"event":"x"}`)
	time.Sleep(200 * time.Millisecond)

	if gotAuth != "Bearer secret-token" {
		t.Fatalf("Authorization = %q, want Bearer secret-token", gotAuth)
	}
}

func TestRelayBodyEnvelope(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		gotBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := newRelay(srv.URL, "")
	r.start()
	defer r.stop()

	r.enqueue("pactify", `{"event_id":"1","type":"test"}`)
	time.Sleep(200 * time.Millisecond)

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

	r := newRelay("http://x", "")

	for i := 0; i < cap+N; i++ {
		r.enqueue("p", `{"i":`+strconv.Itoa(i)+`}`)
	}

	dropped := atomic.LoadInt64(&r.dropped)
	if dropped < int64(N) {
		t.Fatalf("dropped = %d, want >= %d (pushed %d items into cap %d queue)", dropped, N, cap+N, cap)
	}
	if n := r.queueLen(); n > cap {
		t.Fatalf("queue len = %d, want <= %d", n, cap)
	}
}

func TestRelayServerErrorRetriesAndDrops(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := newRelay(srv.URL, "")
	r.start()
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
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		gotBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := newRelay(srv.URL, "")
	r.start()
	defer r.stop()

	r.enqueue("p", `not json`)
	time.Sleep(200 * time.Millisecond)

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
