package cockpit

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type opencodeHTTPError struct {
	status  int
	message string
}

func (e *opencodeHTTPError) Error() string { return e.message }
func (s *opencodeServer) redact(text string) string {
	if s.password == "" {
		return text
	}
	return strings.ReplaceAll(text, s.password, "[redacted]")
}
func (s *opencodeServer) waitReaped(timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-s.reaped:
		return nil
	case <-timer.C:
		return fmt.Errorf("Opencode server did not exit within %s", timeout)
	}
}

// Opencode 1.19 passes its environment to shell tools. Filter its random
// server secret at the adapter boundary, including stderr split across writes.
type secretWriter struct {
	mu              sync.Mutex
	out             io.Writer
	secret, pending string
}

func (w *secretWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending += string(p)
	text := strings.ReplaceAll(w.pending, w.secret, "[redacted]")
	keep := 0
	for n := 1; n < len(w.secret) && n <= len(text); n++ {
		if strings.HasSuffix(text, w.secret[:n]) {
			keep = n
		}
	}
	w.pending = text[len(text)-keep:]
	_, err := io.WriteString(w.out, text[:len(text)-keep])
	return len(p), err
}
func (w *secretWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = io.WriteString(w.out, strings.ReplaceAll(w.pending, w.secret, "[redacted]"))
	w.pending = ""
}
