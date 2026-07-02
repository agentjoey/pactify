package serve

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

func seedStreamProject(t *testing.T, lines int) (root string) {
	t.Helper()
	root = t.TempDir()
	seedProject(t, root, "pactify")
	sdir := filepath.Join(root, ".pact", "orchestrate", "streams")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "line-%d\n", i)
	}
	if err := os.WriteFile(filepath.Join(sdir, "t1.log"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// streamFrames GETs the agent stream (with hdr merged in) and collects data
// lines until the sentinel arrives or the deadline hits.
func streamFrames(t *testing.T, url, sentinel string, hdr map[string]string) []string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	done := make(chan []string, 1)
	go func() {
		var got []string
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data: ") {
				got = append(got, strings.TrimPrefix(line, "data: "))
				if strings.TrimPrefix(line, "data: ") == sentinel {
					done <- got
					return
				}
			}
		}
		done <- got
	}()
	select {
	case got := <-done:
		return got
	case <-time.After(3 * time.Second):
		t.Fatalf("did not receive sentinel %q within deadline", sentinel)
		return nil
	}
}

// A reconnect carrying Last-Event-ID=k must resume at line k+1 — never replay
// the already-delivered backfill (the P0 duplicate-lines-on-reconnect bug).
func TestStreamReconnectResumesPastLastEventID(t *testing.T) {
	root := seedStreamProject(t, 5)
	srv := New([]registry.Project{{Name: "pactify", Path: root}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	got := streamFrames(t, ts.URL+"/api/projects/pactify/orchestrate/stream/t1", "line-5",
		map[string]string{"Last-Event-ID": "3"})
	want := []string{"line-4", "line-5"}
	if len(got) != len(want) {
		t.Fatalf("want exactly %v after Last-Event-ID=3, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}

// A Last-Event-ID at (or past) the end of the file backfills nothing; the
// connection stays open for live appends only.
func TestStreamReconnectAtEndBackfillsNothing(t *testing.T) {
	root := seedStreamProject(t, 5)
	srv := New([]registry.Project{{Name: "pactify", Path: root}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/projects/pactify/orchestrate/stream/t1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", "5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Read for a short window: only the retry/ok preamble may arrive.
	data := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				data <- sc.Text()
				return
			}
		}
	}()
	select {
	case line := <-data:
		t.Fatalf("want no backfill at Last-Event-ID=5, got %q", line)
	case <-time.After(300 * time.Millisecond):
	}
}

// A first connection (no Last-Event-ID) still gets only the trailing tail-200,
// each frame carrying its 1-based line ordinal as the SSE id.
func TestStreamFirstConnectCapsBackfillWithOrdinalIDs(t *testing.T) {
	root := seedStreamProject(t, 250)
	srv := New([]registry.Project{{Name: "pactify", Path: root}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/projects/pactify/orchestrate/stream/t1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	type frame struct{ id, data string }
	done := make(chan []frame, 1)
	go func() {
		var frames []frame
		var cur frame
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
				frames = append(frames, cur)
				if cur.data == "line-250" {
					done <- frames
					return
				}
				cur = frame{}
			}
		}
		done <- frames
	}()
	var frames []frame
	select {
	case frames = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive line-250 within deadline")
	}

	if len(frames) != eventBackfill {
		t.Fatalf("want backfill capped at %d lines, got %d", eventBackfill, len(frames))
	}
	if frames[0].data != "line-51" || frames[0].id != "51" {
		t.Fatalf("want first frame line-51 with id 51, got %+v", frames[0])
	}
	if last := frames[len(frames)-1]; last.data != "line-250" || last.id != "250" {
		t.Fatalf("want last frame line-250 with id 250, got %+v", last)
	}
}

// Live lines appended after the backfill continue the same ordinal sequence —
// the drain path must not restart ids or diverge from the backfill's counter.
func TestDrainStreamFromContinuesOrdinals(t *testing.T) {
	dir := t.TempDir()
	lp := filepath.Join(dir, "s.log")
	if err := os.WriteFile(lp, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	off, ord := drainStreamFrom(&buf, lp, 0, 0)
	if ord != 2 {
		t.Fatalf("want ordinal 2 after two lines, got %d", ord)
	}
	if !strings.Contains(buf.String(), "id: 1\nevent: stream\ndata: a\n") ||
		!strings.Contains(buf.String(), "id: 2\nevent: stream\ndata: b\n") {
		t.Fatalf("want ids 1 and 2 on backlog lines, got: %q", buf.String())
	}

	f, err := os.OpenFile(lp, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("c\n")
	f.Close()

	buf.Reset()
	_, ord = drainStreamFrom(&buf, lp, off, ord)
	if ord != 3 {
		t.Fatalf("want ordinal 3 after append, got %d", ord)
	}
	if !strings.Contains(buf.String(), "id: 3\nevent: stream\ndata: c\n") {
		t.Fatalf("want appended line tagged id 3, got: %q", buf.String())
	}
}
