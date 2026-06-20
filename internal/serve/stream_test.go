package serve

import (
	"bufio"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

// A line written without its trailing newline (the agent is mid-write at poll
// time) must not be emitted as an SSE frame and then re-emitted with the rest as
// a second frame — that splits one agent line across two terminal rows. drainStream
// must hold the partial tail until the newline arrives, then emit it whole once.
func TestDrainStreamKeepsPartialLineWhole(t *testing.T) {
	dir := t.TempDir()
	lp := filepath.Join(dir, "s.log")
	if err := os.WriteFile(lp, []byte("Hello wor"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	off := drainStream(&buf, lp, 0)
	if buf.Len() != 0 {
		t.Fatalf("emitted a partial line before its newline arrived: %q", buf.String())
	}

	f, err := os.OpenFile(lp, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("ld\n")
	f.Close()

	buf.Reset()
	drainStream(&buf, lp, off)
	got := buf.String()
	if n := strings.Count(got, "data:"); n != 1 {
		t.Fatalf("want exactly one frame for the completed line, got %d: %q", n, got)
	}
	if !strings.Contains(got, "data: Hello world\n") {
		t.Fatalf("want the line emitted whole as 'Hello world', got: %q", got)
	}
}

func TestStreamBackfillsAndTails(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "pactify")
	sdir := filepath.Join(root, ".pact", "orchestrate", "streams")
	os.MkdirAll(sdir, 0o755)
	os.WriteFile(filepath.Join(sdir, "t1.log"), []byte("agent boot\n"), 0o644)

	srv := New([]registry.Project{{Name: "pactify", Path: root}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/projects/pactify/orchestrate/stream/t1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	got := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data:") && strings.Contains(sc.Text(), "agent boot") {
				got <- sc.Text()
				return
			}
		}
	}()
	select {
	case <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive backfilled stream line")
	}
}
