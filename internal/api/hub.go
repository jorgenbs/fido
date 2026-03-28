package api

import "sync"

// Event is a server-sent event published through the Hub.
type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

// Hub is an in-process pub/sub for SSE events.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
	// recvToSend maps receive-only channels back to their bidirectional
	// counterpart so Unsubscribe can accept <-chan Event.
	recvToSend map[<-chan Event]chan Event
}

func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[chan Event]struct{}),
		recvToSend:  make(map[<-chan Event]chan Event),
	}
}

func (h *Hub) Subscribe() <-chan Event {
	ch := make(chan Event, 64)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.recvToSend[ch] = ch
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch <-chan Event) {
	h.mu.Lock()
	sendCh, ok := h.recvToSend[ch]
	if ok {
		delete(h.subscribers, sendCh)
		delete(h.recvToSend, ch)
	}
	h.mu.Unlock()
}

func (h *Hub) Publish(evt Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers {
		select {
		case ch <- evt:
		default:
			// slow subscriber — drop event
		}
	}
}
