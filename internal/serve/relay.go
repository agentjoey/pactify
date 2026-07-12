package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentjoey/pactify/internal/cloudauth"
	"github.com/agentjoey/pactify/internal/cloudclient"
	"github.com/agentjoey/pactify/internal/gitx"
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

	// Watermark persistence: per-project uploaded byte offset in log.jsonl.
	// wmMu guards wmPaths, wmMarks, wmDirty, wmPending, and wmLastFlush.
	wmMu        sync.Mutex
	wmPaths     map[string]string // project -> .pact/relay-uploaded.json path
	wmMarks     map[string]int64  // project -> uploaded byte offset
	wmDirty     map[string]bool   // projects with unflushed mark updates
	wmPending   int               // successful posts since last flush
	wmLastFlush time.Time
}

type pactMsg struct {
	project  string
	seq      int64
	line     string
	lineBytes int64 // byte length of the original log.jsonl line (including '\n')
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
		wmPaths:     map[string]string{},
		wmMarks:     map[string]int64{},
		wmDirty:     map[string]bool{},
		wmLastFlush: time.Now(),
	}
	go r.run()
	go r.wmFlushLoop()
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

// replayProject uploads the existing ledger bytes [watermark, upTo) for a
// project with line-index seq, so the board reflects the current state on
// connect (not just future appends). `upTo` is the SSE watcher's seeded offset,
// so replay and the live drain partition the file cleanly (no overlapping/
// duplicate seq). Called once per project before the watch loop starts.
// Idempotent on the relay; safe on a nil relay / missing log.
func (r *relay) replayProject(project, logPath string, upTo int64) {
	if r == nil {
		return
	}
	r.setWatermarkPath(project, logPath)
	f, err := os.Open(logPath)
	if err != nil {
		return
	}
	defer f.Close()

	start := r.loadWatermark(project)
	if fi, err := f.Stat(); err != nil {
		r.resetWatermark(project)
		start = 0
	} else if start < 0 || start > fi.Size() {
		r.resetWatermark(project)
		start = 0
	}

	// seq is the NON-EMPTY line index in log.jsonl and the relay upserts by
	// (projectId, seq) — so the enqueue counter MUST be seeded with the number
	// of lines below the watermark, or the tail would re-use low seqs and
	// overwrite other events' rows on the relay. We therefore scan the already-
	// uploaded prefix locally (disk read only, zero network) to count lines,
	// instead of seeking past it blind.
	rd := bufio.NewReader(f)
	var read int64
	var prefixLines int64
	for read < start {
		line, err := rd.ReadString('\n')
		read += int64(len(line))
		if read > start {
			// Watermark landed mid-line (corrupt/foreign file): fall back to a
			// full idempotent replay from 0.
			r.resetWatermark(project)
			_, _ = f.Seek(0, 0)
			rd = bufio.NewReader(f)
			read = 0
			prefixLines = 0
			break
		}
		if strings.TrimRight(line, "\n") != "" {
			prefixLines++
		}
		if err != nil {
			break
		}
	}
	r.mu.Lock()
	if r.seqs[project] < prefixLines {
		r.seqs[project] = prefixLines
	}
	r.mu.Unlock()

	for read < upTo {
		line, err := rd.ReadString('\n')
		read += int64(len(line))
		if read > upTo {
			break // a trailing partial line past the watcher offset: leave it to drainNew
		}
		if t := strings.TrimRight(line, "\n"); t != "" {
			r.enqueueBlocking(project, t, int64(len(line)))
		}
		if err != nil {
			break
		}
	}
}

// enqueueBlocking assigns the project's next line-index seq and WAITS for queue
// room instead of evicting. Replay uses this: a multi-project boot replay can
// outpace the HTTP consumer by orders of magnitude, and the evicting enqueue
// would silently drop most of the ledger (and misalign the upload watermark,
// degrading every later restart to a full replay). Blocking is safe here —
// replay runs on its own boot goroutine, never on the watch/live path. nil-safe.
func (r *relay) enqueueBlocking(project, line string, lineBytes int64) {
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

	msg := pactMsg{project: project, seq: seq, line: line, lineBytes: lineBytes}
	for {
		// Send only under stopMu — stop() flips `stopped` and closes the queue
		// under the same lock, so this can never send on a closed channel.
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
		case <-r.stopCh:
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// enqueue assigns the project's next line-index seq and drops the event into the
// bounded queue without blocking; a full queue evicts the oldest. nil-safe.
func (r *relay) enqueue(project, line string, lineBytes int64) {
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

	msg := pactMsg{project: project, seq: seq, line: line, lineBytes: lineBytes}
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
	if r.stopped {
		r.stopMu.Unlock()
		return
	}
	r.stopped = true
	close(r.stopCh)
	close(r.queue)
	r.stopMu.Unlock()
	<-r.done
	r.wmMu.Lock()
	r.flushWatermarksLocked()
	r.wmMu.Unlock()
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
			r.advanceWatermark(msg.project, msg.lineBytes)
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

// setWatermarkPath records the path of the per-project watermark file
// (.pact/relay-uploaded.json) from the project's log.jsonl path. nil-safe.
func (r *relay) setWatermarkPath(project, logPath string) {
	if r == nil {
		return
	}
	root := filepath.Dir(filepath.Dir(logPath))
	path := filepath.Join(root, ".pact", "relay-uploaded.json")
	// The watermark is machine-local runtime state, but users COMMIT .pact/
	// (that's the protocol) — route it through .git/info/exclude on every repo
	// this serve touches, same as orchestrate's runtime artifacts (spec P0b).
	_ = gitx.EnsureExcluded(root, ".pact/relay-uploaded.json")
	r.wmMu.Lock()
	r.wmPaths[project] = path
	r.wmMu.Unlock()
}

// loadWatermark returns the persisted uploaded byte offset for project,
// defaulting to 0 when no watermark file exists yet. Caller should hold wmMu
// or be prepared to tolerate races; replayProject loads under wmMu.
func (r *relay) loadWatermark(project string) int64 {
	r.wmMu.Lock()
	defer r.wmMu.Unlock()
	if mark, ok := r.wmMarks[project]; ok {
		return mark
	}
	path := r.wmPaths[project]
	if path == "" {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var m map[string]int64
	if err := json.Unmarshal(data, &m); err != nil {
		return 0
	}
	mark := m[project]
	if mark < 0 {
		mark = 0
	}
	r.wmMarks[project] = mark
	return mark
}

// resetWatermark zeroes the in-memory and on-disk watermark for project.
// Used as an idempotent fallback when the file shrank or became unreadable.
func (r *relay) resetWatermark(project string) {
	if r == nil {
		return
	}
	r.wmMu.Lock()
	defer r.wmMu.Unlock()
	r.wmMarks[project] = 0
	r.wmDirty[project] = true
	r.flushWatermarksLocked()
}

// advanceWatermark moves the uploaded byte offset for project by n bytes after
// a successful POST, then throttles disk writes to every 32 lines or 2 seconds.
func (r *relay) advanceWatermark(project string, n int64) {
	if r == nil || n <= 0 {
		return
	}
	r.wmMu.Lock()
	defer r.wmMu.Unlock()
	r.wmMarks[project] += n
	r.wmDirty[project] = true
	r.wmPending++
	if r.wmPending >= 32 || time.Since(r.wmLastFlush) >= 2*time.Second {
		r.flushWatermarksLocked()
	}
}

// wmFlushLoop periodically flushes dirty watermarks every 2 seconds.
func (r *relay) wmFlushLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.wmMu.Lock()
			r.flushWatermarksLocked()
			r.wmMu.Unlock()
		case <-r.stopCh:
			return
		}
	}
}

// flushWatermarksLocked writes all dirty project watermarks to their
// respective .pact/relay-uploaded.json files via atomic tmp+rename. Existing
// orphan keys are preserved. Caller must hold wmMu.
func (r *relay) flushWatermarksLocked() {
	if len(r.wmDirty) == 0 {
		r.wmLastFlush = time.Now()
		return
	}
	byPath := make(map[string]map[string]int64)
	for project := range r.wmDirty {
		path := r.wmPaths[project]
		if path == "" {
			continue
		}
		if _, ok := byPath[path]; !ok {
			existing := make(map[string]int64)
			if data, err := os.ReadFile(path); err == nil {
				_ = json.Unmarshal(data, &existing)
			}
			byPath[path] = existing
		}
		byPath[path][project] = r.wmMarks[project]
	}
	for path, marks := range byPath {
		_ = atomicWriteJSON(path, marks)
	}
	r.wmDirty = make(map[string]bool)
	r.wmPending = 0
	r.wmLastFlush = time.Now()
}

// atomicWriteJSON writes v to path as JSON via a temp file in the same
// directory plus rename, with 0o600 permissions.
func atomicWriteJSON(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	failed := true
	defer func() {
		if failed {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	failed = false
	return nil
}
