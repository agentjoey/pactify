package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentjoey/pactify/internal/cloudauth"
	"github.com/agentjoey/pactify/internal/cloudclient"
	"github.com/agentjoey/pactify/internal/event"
)

// relay uploads pact ledger events to the shared relay as E2E-encrypted wire
// envelopes (U2 Mission Control). Best-effort & async: it NEVER blocks the
// caller and NEVER propagates failures back to the watcher.
//
// Each event becomes a POST /v1/pact/ingest: a cleartext operational header
// (project/eventType/task/feature/seq/ts) that drives the cross-machine board,
// plus `bodyEnc` — the full event JSON encrypted under a per-project key derived
// from the account master secret, which the relay can never derive.
//
// seq is the event's line index in `.pact/log.jsonl` (append-only → stable
// across restarts). On (re)connect the full ledger is replayed with those
// indices; the relay's idempotent (projectId, seq) upsert makes re-pushes no-ops.
type relay struct {
	endpoint  string // <baseURL>/v1/pact/ingest
	accountID string
	master    []byte
	client    *cloudclient.Client // for token refresh on 401

	mu          sync.Mutex
	token       string
	seqs        map[string]int64  // next line index per project
	projectKeys map[string][]byte // cache: projectID → per-project key

	stopMu  sync.Mutex // guards stopped and serializes close(queue) with sends
	stopped bool
	queue   chan pactMsg
	stopCh  chan struct{}
	done    chan struct{}
	dropped int64
	http    *http.Client
}

type pactMsg struct {
	project string
	seq     int64
	line    string
}

// newRelay builds the pact-event uploader for baseURL. A "" baseURL returns nil
// (relay disabled). It requires a cloud session (`pactify account login`) and the
// account master secret; if either is missing it returns an error so the caller
// can warn and run without the relay.
func newRelay(baseURL, _ string) (*relay, error) {
	if baseURL == "" {
		return nil, nil
	}
	sess, err := cloudclient.LoadSession()
	if err != nil {
		return nil, fmt.Errorf("relay: no cloud session (run `pactify account login`): %w", err)
	}
	master, err := cloudauth.LoadMasterSecret()
	if err != nil {
		return nil, fmt.Errorf("relay: %w", err)
	}
	base := strings.TrimRight(baseURL, "/")
	r := &relay{
		endpoint:    base + "/v1/pact/ingest",
		accountID:   sess.AccountID,
		master:      master,
		client:      cloudclient.New(base),
		token:       sess.Token,
		seqs:        map[string]int64{},
		projectKeys: map[string][]byte{},
		queue:       make(chan pactMsg, 256),
		stopCh:      make(chan struct{}),
		done:        make(chan struct{}),
		http:        &http.Client{Timeout: 10 * time.Second},
	}
	go r.run()
	return r, nil
}

// projectID for a serve project id (a name slug): account-scoped and stable.
func (r *relay) projectID(project string) string { return r.accountID + ":" + project }

// projectKey returns the cached per-project encryption key, deriving it once.
func (r *relay) projectKey(project string) ([]byte, error) {
	pid := r.projectID(project)
	r.mu.Lock()
	if k, ok := r.projectKeys[pid]; ok {
		r.mu.Unlock()
		return k, nil
	}
	r.mu.Unlock()
	k, err := cloudauth.DeriveProjectKey(r.master, pid)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.projectKeys[pid] = k
	r.mu.Unlock()
	return k, nil
}

// replayProject uploads the existing ledger bytes [0, upTo) for a project with
// line-index seq, so the board reflects the current state on connect (not just
// future appends). `upTo` is the SSE watcher's seeded offset, so replay and the
// live drain partition the file cleanly (no overlapping/duplicate seq). Called
// once per project before the watch loop starts. Idempotent on the relay; safe
// on a nil relay / missing log.
func (r *relay) replayProject(project, logPath string, upTo int64) {
	if r == nil {
		return
	}
	f, err := os.Open(logPath)
	if err != nil {
		return
	}
	defer f.Close()
	rd := bufio.NewReader(f)
	var read int64
	for read < upTo {
		line, err := rd.ReadString('\n')
		read += int64(len(line))
		if read > upTo {
			break // a trailing partial line past the watcher offset: leave it to drainNew
		}
		if t := strings.TrimRight(line, "\n"); t != "" {
			r.enqueue(project, t)
		}
		if err != nil {
			break
		}
	}
}

// enqueue assigns the project's next line-index seq and drops the event into the
// bounded queue without blocking; a full queue evicts the oldest. nil-safe.
func (r *relay) enqueue(project, line string) {
	if r == nil {
		return
	}
	r.stopMu.Lock()
	if r.stopped {
		r.stopMu.Unlock()
		return
	}
	r.mu.Lock()
	seq := r.seqs[project]
	r.seqs[project] = seq + 1
	r.mu.Unlock()
	r.stopMu.Unlock()

	msg := pactMsg{project: project, seq: seq, line: line}
	for {
		r.stopMu.Lock()
		if r.stopped {
			r.stopMu.Unlock()
			return
		}
		select {
		case r.queue <- msg:
			r.stopMu.Unlock()
			return
		default:
			r.stopMu.Unlock()
		}
		select {
		case <-r.queue:
			atomic.AddInt64(&r.dropped, 1)
		case <-r.stopCh:
			return
		default:
		}
	}
}

func (r *relay) stop() {
	r.stopMu.Lock()
	r.stopped = true
	close(r.stopCh)
	close(r.queue)
	r.stopMu.Unlock()
	<-r.done
}

func (r *relay) droppedCount() int64 { return atomic.LoadInt64(&r.dropped) }
func (r *relay) queueLen() int       { return len(r.queue) }

func (r *relay) run() {
	defer close(r.done)
	for msg := range r.queue {
		r.postOne(msg)
	}
}

// pactIngestBody is the POST /v1/pact/ingest wire contract (mirrors the relay's
// PactIngestRequest zod schema).
type pactIngestBody struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Feature   string `json:"feature,omitempty"`
	EventType string `json:"eventType"`
	Task      string `json:"task,omitempty"`
	Seq       int64  `json:"seq"`
	EventID   string `json:"eventId,omitempty"`
	TS        int64  `json:"ts"`
	BodyEnc   string `json:"bodyEnc"`
}

// buildBody turns one ledger line into the encrypted ingest envelope: cleartext
// operational header parsed from the event + `bodyEnc` = the full line encrypted
// under the per-project key. Returns nil to skip a line that can't be built
// (unparseable / encrypt error) rather than send garbage.
func (r *relay) buildBody(msg pactMsg) []byte {
	var ev event.Event
	if err := json.Unmarshal([]byte(msg.line), &ev); err != nil || ev.EventType == "" {
		return nil
	}
	key, err := r.projectKey(msg.project)
	if err != nil {
		return nil
	}
	blob, err := cloudauth.EncryptEvent(key, []byte(msg.line))
	if err != nil {
		return nil
	}
	bodyEnc, err := json.Marshal(blob)
	if err != nil {
		return nil
	}
	var tsMs int64
	if t, err := time.Parse(time.RFC3339, ev.TS); err == nil {
		tsMs = t.UnixMilli()
	}
	body, _ := json.Marshal(pactIngestBody{
		ProjectID: r.projectID(msg.project),
		Name:      msg.project,
		Feature:   ev.Feature,
		EventType: ev.EventType,
		Task:      ev.TaskID,
		Seq:       msg.seq,
		EventID:   ev.EventID,
		TS:        tsMs,
		BodyEnc:   string(bodyEnc),
	})
	return body
}

// postOne delivers one event, retrying with capped backoff. On a 401 it refreshes
// the token once (re-login with the master secret) before retrying. Final failure
// increments dropped.
func (r *relay) postOne(msg pactMsg) {
	body := r.buildBody(msg)
	if body == nil {
		return
	}
	refreshed := false
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
		req, err := http.NewRequest("POST", r.endpoint, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		r.mu.Lock()
		tok := r.token
		r.mu.Unlock()
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := r.http.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return
		}
		if resp.StatusCode == http.StatusUnauthorized && !refreshed {
			refreshed = true
			r.refreshToken()
		}
	}
	atomic.AddInt64(&r.dropped, 1)
}

// refreshToken re-authenticates with the master secret to get a fresh bearer
// token (the cached one expired). Best-effort — a failure just leaves the old
// token and the event drops after its retries.
func (r *relay) refreshToken() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sess, err := r.client.Authenticate(ctx, r.master)
	if err != nil {
		return
	}
	r.mu.Lock()
	r.token = sess.Token
	r.mu.Unlock()
	_ = cloudclient.SaveSession(sess)
}
