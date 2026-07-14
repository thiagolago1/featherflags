package server

import "sync"

// hub fans out "something changed in project X" signals to SSE subscribers.
// Events carry no payload: clients re-evaluate, which keeps per-user rollout
// and condition logic on the server.
type hub struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{} // projectID -> subscriber channels
}

func newHub() *hub {
	return &hub{subs: map[string]map[chan struct{}]struct{}{}}
}

func (h *hub) subscribe(projectID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[projectID] == nil {
		h.subs[projectID] = map[chan struct{}]struct{}{}
	}
	h.subs[projectID][ch] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		delete(h.subs[projectID], ch)
		if len(h.subs[projectID]) == 0 {
			delete(h.subs, projectID)
		}
		h.mu.Unlock()
	}
	return ch, cancel
}

// broadcast never blocks: each subscriber channel has capacity 1, and a
// pending signal means the subscriber will re-evaluate anyway.
func (h *hub) broadcast(projectID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[projectID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
