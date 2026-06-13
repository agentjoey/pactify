package serve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type relay struct {
	url     string
	token   string
	queue   chan relayMsg
	done    chan struct{}
	dropped int64
	client  *http.Client
}

type relayMsg struct {
	project string
	line    string
}

func newRelay(url, token string) *relay {
	if url == "" {
		return nil
	}
	return &relay{
		url:    url,
		token:  token,
		queue:  make(chan relayMsg, 256),
		done:   make(chan struct{}),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (r *relay) start() {
	go r.run()
}

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
			select {
			case <-r.queue:
				atomic.AddInt64(&r.dropped, 1)
			default:
			}
		}
	}
}

func (r *relay) stop() {
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

func (r *relay) postOne(msg relayMsg) {
	body := r.buildBody(msg)

	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			if backoff > 4*time.Second {
				backoff = 4 * time.Second
			}
			time.Sleep(backoff)
		}
		req, err := http.NewRequest("POST", r.url, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if r.token != "" {
			req.Header.Set("Authorization", "Bearer "+r.token)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return
		}
		lastErr = fmt.Errorf("status %d", resp.StatusCode)
	}
	_ = lastErr
	atomic.AddInt64(&r.dropped, 1)
}

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
