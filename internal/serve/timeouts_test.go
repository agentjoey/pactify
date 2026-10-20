package serve

import (
	"net/http"
	"testing"
	"time"
)

func TestDashboardHTTPTimeouts(t *testing.T) {
	server := dashboardHTTPServer("127.0.0.1:7777", http.NotFoundHandler())
	if server.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 10s", server.ReadHeaderTimeout)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout = %v, want 60s", server.IdleTimeout)
	}
	if server.WriteTimeout != 0 {
		t.Fatal("SSE must not have a response-wide write deadline")
	}
}
