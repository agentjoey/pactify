package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

// relay POSTs serve log events to a configured endpoint, best-effort & async.
// NEVER blocks the caller; NEVER propagates failures back to the watcher.
type relay struct {
	url     string
	token   string
	queue   chan relayMsg
	stopCh  chan struct{}
	done    chan struct{}
	dropped int64
	client  *http.Client
}

type relayMsg struct {
	project string
	line    string
}

// newRelay builds a relay posting to url with optional bearer token.
// A "" url returns nil (relay disabled). Otherwise starts the background POST
// goroutine immediately — callers never need a separate start step.
func newRelay(url, token string) *relay {
	if url == "" {
		return nil
	}
	r := &relay{
		url:    url,
		token:  token,
		queue:  make(chan relayMsg, 256),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
		client: &http.Client{Timeout: 10 * time.Second},
	}
	go r.run()
	return r
}

// enqueue drops a log line into the bounded queue without blocking.
// When the queue is full the oldest item is evicted and dropped is incremented.
// Calling on a nil relay is a safe no-op.
func (r *relay) enqueue(project, line string) {
	if r == nil {
		return
	}
	msg := relayMsg{project, line}
	for {
		select {
		case r.queue <- msg:
			return
		default:
			// Queue full: evict the oldest item to make room.
			select {
			case <-r.queue:
				atomic.AddInt64(&r.dropped, 1)
			default:
			}
		}
	}
}

// stop closes the queue, signals the background goroutine to exit, and waits
// for it to finish.
func (r *relay) stop() {
	close(r.stopCh)
	close(r.queue)
	<-r.done
}

func (r *relay) droppedCount() int64 {
	return atomic.LoadInt64(&r.dropped)
}

func (r *relay) queueLen() int {
	return len(r.queue)
}

func (r *relay) run() {
	defer close(r.done)
	for msg := range r.queue {
		r.postOne(msg)
	}
}

// postOne delivers a single message to the configured endpoint.
// Retries up to 3 times (4 attempts total) with capped exponential backoff
// (1s, 2s, 4s). Final failure increments dropped. Checks stopCh between
// attempts so stop() can interrupt without waiting for all retries.
func (r *relay) postOne(msg relayMsg) {
	body := r.buildBody(msg)

	for attempt := 0; attempt < 4; attempt++ {
		select {
		case <-r.stopCh:
			return
		default:
		}
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			if backoff > 4*time.Second {
				backoff = 4 * time.Second
			}
			select {
			case <-time.After(backoff):
			case <-r.stopCh:
				return
			}
		}
		req, err := http.NewRequest("POST", r.url, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if r.token != "" {
			req.Header.Set("Authorization", "Bearer "+r.token)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return
		}
	}
	atomic.AddInt64(&r.dropped, 1)
}

// buildBody wraps a log line into the relay envelope: {"project":<id>,"event":<json>}.
// If line is not valid JSON it is quoted as a JSON string and the raw text is
// carried as the event value — never panics.
func (r *relay) buildBody(msg relayMsg) []byte {
	var event json.RawMessage
	if err := json.Unmarshal([]byte(msg.line), &event); err != nil {
		quoted, _ := json.Marshal(msg.line)
		event = json.RawMessage(quoted)
	}
	envelope := struct {
		Project string          `json:"project"`
		Event   json.RawMessage `json:"event"`
	}{Project: msg.project, Event: event}
	b, _ := json.Marshal(envelope)
	return b
}
