package serve

import (
	"context"
	"github.com/agentjoey/pactify/internal/sub"
	"net"
	"net/http"
	"time"
)

// Run starts the HTTP server on addr and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context, addr string) error {
	security, err := newSecurityConfig(addr)
	if err != nil {
		return err
	}
	// Bind the validated IP, avoiding a second hostname lookup before listening.
	ln, err := net.ListenTCP("tcp", security.listenAddr)
	if err != nil {
		return err
	}
	defer ln.Close()
	// Port zero must use the actual listening port in Host and Origin checks.
	security.listenAddr = ln.Addr().(*net.TCPAddr)
	httpSrv := dashboardHTTPServer(addr, s.handler(security))
	stopped := make(chan struct{})
	defer close(stopped)
	maintenance, cancel := context.WithCancel(ctx)
	defer cancel()
	if store, err := sub.DefaultStore(); err == nil {
		go store.MaintainStorage(maintenance)
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = httpSrv.Close()
		case <-stopped:
		}
	}()
	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func dashboardHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
}
