package serve

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cloudauth"
	"github.com/agentjoey/pactify/internal/cloudclient"
)

// goldenMaster is the fixed 32-byte secret (00..1f) used across cloud golden tests.
func goldenMaster() []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// seedRelaySession sets up a throwaway HOME with the golden master secret + a
// cached session pointing at relayURL, so SetRelay(relayURL, "") enables the relay.
func seedRelaySession(t *testing.T, relayURL string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PACTIFY_MASTER_SECRET", hex.EncodeToString(goldenMaster()))
	if err := cloudclient.SaveSession(&cloudclient.Session{
		RelayURL: relayURL, AccountID: "acct1", Token: "tok",
	}); err != nil {
		t.Fatal(err)
	}
}

// ingestCapture is one decoded POST /v1/pact/ingest body.
type ingestCapture struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Feature   string `json:"feature"`
	EventType string `json:"eventType"`
	Task      string `json:"task"`
	Seq       int64  `json:"seq"`
	TS        int64  `json:"ts"`
	BodyEnc   string `json:"bodyEnc"`
}

// newTestRelay wires a relay to an httptest ingest endpoint, with a throwaway
// session + the golden master secret in a temp HOME. The channel receives each POST.
func newTestRelay(t *testing.T) (*relay, chan ingestCapture) {
	t.Helper()
	got := make(chan ingestCapture, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/pact/ingest" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var c ingestCapture
		_ = json.NewDecoder(r.Body).Decode(&c)
		got <- c
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	seedRelaySession(t, srv.URL)
	r, err := newRelay(srv.URL, "")
	if err != nil {
		t.Fatalf("newRelay: %v", err)
	}
	t.Cleanup(r.stop)
	return r, got
}

func recv(t *testing.T, ch chan ingestCapture) ingestCapture {
	t.Helper()
	select {
	case c := <-ch:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ingest POST")
		return ingestCapture{}
	}
}

func TestRelayNilOnEmptyURL(t *testing.T) {
	r, err := newRelay("", "")
	if r != nil || err != nil {
		t.Fatalf("newRelay(\"\") = %v, %v; want nil, nil", r, err)
	}
}

func TestRelayNilEnqueueNoPanic(t *testing.T) {
	var r *relay
	r.enqueue("p", `{}`, 0)         // must not panic
	r.replayProject("p", "x", 0) // must not panic
}

func TestRelayEnvelopeHeaderAndEncryptedBody(t *testing.T) {
	r, got := newTestRelay(t)
	line := `{"event_id":"e1","ts":"2026-06-13T02:59:28Z","agent_id":"claude","role":"orchestrator","event_type":"checkpoint","task_id":"m0-wire","feature":"cloud-m0","payload":{"spec":"SECRET-EVIDENCE"}}`
	r.enqueue("pactify", line, int64(len(line)))
	c := recv(t, got)

	// Cleartext operational header for the board.
	if c.ProjectID != "acct1:pactify" || c.Name != "pactify" {
		t.Fatalf("project header wrong: %+v", c)
	}
	if c.EventType != "checkpoint" || c.Task != "m0-wire" || c.Feature != "cloud-m0" {
		t.Fatalf("event header wrong: %+v", c)
	}
	if c.Seq != 0 {
		t.Fatalf("seq = %d, want 0", c.Seq)
	}
	want := time.Date(2026, 6, 13, 2, 59, 28, 0, time.UTC).UnixMilli()
	if c.TS != want {
		t.Fatalf("ts = %d, want %d", c.TS, want)
	}
	// The sensitive body is ciphertext, not the plaintext line.
	if c.BodyEnc == "" || c.BodyEnc == line || strings.Contains(c.BodyEnc, "SECRET-EVIDENCE") {
		t.Fatalf("bodyEnc leaks plaintext or is empty")
	}
	// And it decrypts back to the exact line under the per-project key.
	key, _ := cloudauth.DeriveProjectKey(goldenMaster(), "acct1:pactify")
	var blob cloudauth.EncryptedBlob
	if err := json.Unmarshal([]byte(c.BodyEnc), &blob); err != nil {
		t.Fatalf("bodyEnc not an EncryptedBlob: %v", err)
	}
	pt, err := cloudauth.DecryptEvent(key, blob)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(pt) != line {
		t.Fatalf("decrypted body != original line")
	}
}

func TestRelaySeqIncrementsPerProject(t *testing.T) {
	r, got := newTestRelay(t)
	line := `{"event_type":"assign","ts":"2026-06-13T02:59:28Z"}`
	for i := 0; i < 3; i++ {
		r.enqueue("proj", line, int64(len(line)))
	}
	for want := int64(0); want < 3; want++ {
		if c := recv(t, got); c.Seq != want {
			t.Fatalf("seq = %d, want %d", c.Seq, want)
		}
	}
}

func TestRelayReplayProjectUploadsExistingLedger(t *testing.T) {
	r, got := newTestRelay(t)
	dir := t.TempDir()
	lp := filepath.Join(dir, "log.jsonl")
	content := `{"event_type":"assign","ts":"2026-06-13T02:59:28Z","task_id":"t1"}` + "\n" +
		`{"event_type":"checkpoint","ts":"2026-06-13T03:01:45Z","task_id":"t1"}` + "\n"
	if err := os.WriteFile(lp, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	r.replayProject("proj", lp, int64(len(content)))
	a, b := recv(t, got), recv(t, got)
	if a.Seq != 0 || b.Seq != 1 {
		t.Fatalf("replay seqs = %d,%d want 0,1", a.Seq, b.Seq)
	}
	if a.EventType != "assign" || b.EventType != "checkpoint" {
		t.Fatalf("replay order wrong: %s,%s", a.EventType, b.EventType)
	}
}

// lineJSON returns a compact checkpoint event line with the given task_id.
func lineJSON(taskID string) string {
	return `{"event_type":"checkpoint","ts":"2026-06-13T02:59:28Z","task_id":"` + taskID + `"}` + "\n"
}

func TestRelayWatermarkPersistsAndResumesIncremental(t *testing.T) {
	dir := t.TempDir()
	lp := filepath.Join(dir, ".pact", "log.jsonl")
	if err := os.MkdirAll(filepath.Dir(lp), 0o700); err != nil {
		t.Fatal(err)
	}
	line1 := lineJSON("t1")
	line2 := lineJSON("t2")
	if err := os.WriteFile(lp, []byte(line1+line2), 0o600); err != nil {
		t.Fatal(err)
	}

	// First relay: replay uploads both lines and flushes watermark on stop.
	r1, got1 := newTestRelay(t)
	r1.replayProject("proj", lp, int64(len(line1)+len(line2)))
	recv(t, got1)
	recv(t, got1)
	r1.stop()

	wmPath := filepath.Join(dir, ".pact", "relay-uploaded.json")
	if data, err := os.ReadFile(wmPath); err != nil {
		t.Fatalf("watermark file missing after stop: %v", err)
	} else if !strings.Contains(string(data), `"proj": `+strconv.Itoa(len(line1)+len(line2))) {
		t.Fatalf("watermark content wrong: %s", data)
	}

	// Append a third line and start a fresh relay.
	line3 := lineJSON("t3")
	if err := os.WriteFile(lp, []byte(line1+line2+line3), 0o600); err != nil {
		t.Fatal(err)
	}
	r2, got2 := newTestRelay(t)
	r2.replayProject("proj", lp, int64(len(line1)+len(line2)+len(line3)))
	// Only the new line should be uploaded.
	c := recv(t, got2)
	if c.Task != "t3" {
		t.Fatalf("expected only t3, got %+v", c)
	}
	// seq must continue from the prefix line count: the relay upserts by
	// (projectId, seq), so a restart that re-used low seqs for tail lines
	// would overwrite other events' rows.
	if c.Seq != 2 {
		t.Fatalf("resumed seq = %d, want 2 (after 2 watermarked lines)", c.Seq)
	}
	select {
	case <-got2:
		t.Fatal("unexpected extra ingest")
	case <-time.After(200 * time.Millisecond):
	}
	r2.stop()
}

func TestRelayWatermarkTruncationResetsToFullReplay(t *testing.T) {
	dir := t.TempDir()
	lp := filepath.Join(dir, ".pact", "log.jsonl")
	if err := os.MkdirAll(filepath.Dir(lp), 0o700); err != nil {
		t.Fatal(err)
	}
	line1 := lineJSON("t1")
	line2 := lineJSON("t2")
	// Seed a watermark beyond the file size.
	if err := os.WriteFile(lp, []byte(line1+line2), 0o600); err != nil {
		t.Fatal(err)
	}
	wmPath := filepath.Join(dir, ".pact", "relay-uploaded.json")
	if err := os.WriteFile(wmPath, []byte(`{"proj": 999999}`), 0o600); err != nil {
		t.Fatal(err)
	}

	r, got := newTestRelay(t)
	r.replayProject("proj", lp, int64(len(line1)+len(line2)))
	a, b := recv(t, got), recv(t, got)
	if a.Task != "t1" || b.Task != "t2" {
		t.Fatalf("expected full replay, got %+v %+v", a, b)
	}
	r.stop()

	// After reset + full replay + successful posts, the watermark lands at the
	// end of the successfully uploaded bytes.
	want := len(line1) + len(line2)
	if data, err := os.ReadFile(wmPath); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(data), `"proj": `+strconv.Itoa(want)) {
		t.Fatalf("watermark should be %d, got %s", want, data)
	}
}

func TestRelayFailedPostDoesNotAdvanceWatermark(t *testing.T) {
	got := make(chan ingestCapture, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/pact/ingest" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var c ingestCapture
		_ = json.NewDecoder(r.Body).Decode(&c)
		got <- c
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	seedRelaySession(t, srv.URL)
	r, err := newRelay(srv.URL, "")
	if err != nil {
		t.Fatalf("newRelay: %v", err)
	}
	t.Cleanup(r.stop)

	dir := t.TempDir()
	lp := filepath.Join(dir, ".pact", "log.jsonl")
	if err := os.MkdirAll(filepath.Dir(lp), 0o700); err != nil {
		t.Fatal(err)
	}
	r.setWatermarkPath("proj", lp)

	line := lineJSON("t1")
	r.enqueue("proj", line, int64(len(line)))

	// Wait for the four retry attempts.
	for i := 0; i < 4; i++ {
		select {
		case <-got:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for failed attempt")
		}
	}

	// No successful posts => no watermark file should be written.
	if _, err := os.Stat(filepath.Join(dir, ".pact", "relay-uploaded.json")); !os.IsNotExist(err) {
		t.Fatal("watermark file should not exist after failures")
	}
}

func TestRelayWatermarkAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "relay-uploaded.json")
	marks := map[string]int64{"proj": 42}
	if err := atomicWriteJSON(path, marks); err != nil {
		t.Fatalf("atomicWriteJSON: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0o600", fi.Mode().Perm())
	}
	// No leftover temp files.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".relay-uploaded.json.tmp.") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}
