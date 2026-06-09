package serve

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

func TestServesEmbeddedDashboard(t *testing.T) {
	srv := New([]registry.Project{{Name: "demo", Path: t.TempDir()}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("/ status=%d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(strings.ToLower(string(b)), "<!doctype html") && !strings.Contains(string(b), "<div id=\"root\">") {
		t.Fatalf("/ did not serve the SPA index: %.120s", b)
	}

	// SPA fallback: an unknown non-API path returns index.html, not 404
	resp2, err2 := http.Get(ts.URL + "/some/spa/route")
	if err2 != nil {
		t.Fatal(err2)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("SPA fallback status=%d", resp2.StatusCode)
	}
}
