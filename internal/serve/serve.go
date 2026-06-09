package serve

import (
	"context"
	"net/http"
)

// Run starts the HTTP server on addr and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context, addr string) error {
	if err := s.StartWatchers(); err != nil {
		return err
	}
	defer s.Stop()
	httpSrv := &http.Server{Addr: addr, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
