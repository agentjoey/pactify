package serve

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
)

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
