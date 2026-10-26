// Package sse is the skeleton of the Server-Sent Events hub. Only the
// in-process subscriber registry exists; the HTTP stream handler, heartbeat,
// Last-Event-ID replay from Redis and RabbitMQ fan-out come with the
// real-time cycle.
package sse

import "sync"

// Event is one message pushed to a subscriber.
type Event struct {
	ID   string
	Type string
	Data []byte
}

// Hub keeps the open subscriptions of this instance, keyed by user id.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan Event]struct{}
	bufferSize  int
}

func NewHub(bufferSize int) *Hub {
	return &Hub{subscribers: make(map[string]map[chan Event]struct{}), bufferSize: bufferSize}
}

// Subscribe registers a stream for userID. Call the returned func to leave.
func (h *Hub) Subscribe(userID string) (<-chan Event, func()) {
	ch := make(chan Event, h.bufferSize)

	h.mu.Lock()
	if h.subscribers[userID] == nil {
		h.subscribers[userID] = make(map[chan Event]struct{})
	}
	h.subscribers[userID][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers[userID], ch)
			if len(h.subscribers[userID]) == 0 {
				delete(h.subscribers, userID)
			}
			h.mu.Unlock()
			close(ch)
		})
	}
}

// Publish sends evt to every stream of userID. Slow subscribers whose buffer
// is full miss the event and must recover it through Last-Event-ID replay.
func (h *Hub) Publish(userID string, evt Event) (delivered int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers[userID] {
		select {
		case ch <- evt:
			delivered++
		default:
		}
	}
	return delivered
}

// Subscribers returns the number of open streams on this instance.
func (h *Hub) Subscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, subs := range h.subscribers {
		n += len(subs)
	}
	return n
}
