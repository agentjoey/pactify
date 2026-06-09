package serve

import "sync"

// hub fans out per-project event JSON lines to SSE subscribers.
type hub struct {
	mu   sync.Mutex
	subs map[string]map[chan string]struct{} // projectID -> set of channels
}

func newHub() *hub { return &hub{subs: map[string]map[chan string]struct{}{}} }

func (h *hub) subscribe(id string) chan string {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan string, 32)
	if h.subs[id] == nil {
		h.subs[id] = map[chan string]struct{}{}
	}
	h.subs[id][ch] = struct{}{}
	return ch
}

func (h *hub) unsubscribe(id string, ch chan string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m := h.subs[id]; m != nil {
		delete(m, ch)
	}
	close(ch)
}

func (h *hub) broadcast(id, line string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[id] {
		select {
		case ch <- line:
		default: // drop if a slow subscriber's buffer is full
		}
	}
}
